// Package e2e runs end-to-end tests against the real virtualization engine.
// These tests need a working Docker-compatible engine socket (podman or
// docker) with permission to create containers, iptables rules and sysctls —
// they are skipped automatically when the socket is missing.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"example.com/codetest/agent/internal/api"
	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/provider"
	"example.com/codetest/agent/internal/service"
)

// engineSocket returns the path of a usable engine socket, or "".
func engineSocket() string {
	for _, p := range []string{"/run/podman/podman.sock", "/var/run/docker.sock"} {
		if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return p
		}
	}
	return ""
}

func requireEngine(t *testing.T) string {
	t.Helper()
	sock := engineSocket()
	if sock == "" {
		t.Skip("no docker-compatible engine socket (/run/podman/podman.sock, /var/run/docker.sock) — skipping real-engine E2E")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman CLI not found — skipping real-engine E2E")
	}
	return sock
}

func runPodman(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("podman %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

type agent struct {
	ts  *httptest.Server
	svc *service.Service
	dir string
}

// bootAgent starts a real agent against the real engine and runs the same
// boot sequence as cmd/agent/main.go: RebuildNAT, EnsureRestartPolicies,
// EnsureIPv6 (no-op with empty mode). Reusing the same data dir across boots
// simulates the agent process surviving a host reboot (state.json persists).
func bootAgent(t *testing.T, dataDir, token, sock string) *agent {
	t.Helper()
	cfg := config.Config{
		Listen:     "127.0.0.1:0",
		Token:      token,
		DataDir:    dataDir,
		VirtType:   "oci",
		SocketPath: sock,
		CLIBin:     "podman",
		WanIface:   defaultIface(),
		PortStart:  21000,
		PortEnd:    21100,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	prov, err := provider.New("oci", sock, provider.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(cfg, prov, logger)
	// Mirror main.go's boot order.
	if err := svc.RebuildNAT(); err != nil {
		t.Logf("rebuild NAT (best effort): %v", err)
	}
	if rp, ok := prov.(interface{ EnsureRestartPolicies(context.Context) error }); ok {
		if err := rp.EnsureRestartPolicies(context.Background()); err != nil {
			t.Logf("ensure restart policies (best effort): %v", err)
		}
	}
	if err := svc.EnsureIPv6(); err != nil {
		t.Logf("ipv6 setup (best effort): %v", err)
	}
	ts := httptest.NewServer(api.New(cfg, svc))
	t.Cleanup(ts.Close)
	return &agent{ts: ts, svc: svc, dir: dataDir}
}

func (a *agent) api(method, path, token string, body any) (*http.Response, []byte) {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, a.ts.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.ts.Client().Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp, buf.Bytes()
}

type envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// createInstance creates a real container through the agent REST API.
func (a *agent) createInstance(t *testing.T, token, name string) {
	t.Helper()
	resp, body := a.api(http.MethodPost, "/agent/v1/instances", token, map[string]any{
		"name": name, "image": "alpine:latest",
		"cpu_cores": 1, "memory_mb": 256, "disk_mb": 0, "swap_mb": 0, "ipv6": false,
	})
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("create instance: status=%v body=%s", resp, body)
	}
}

// instanceStatus reads the agent's view of an instance.
func (a *agent) instanceStatus(t *testing.T, token, name string) string {
	t.Helper()
	resp, body := a.api(http.MethodGet, "/agent/v1/instances/"+name, token, nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		return "unknown"
	}
	var env envelope
	var info provider.Info
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatal(err)
	}
	return info.Status
}

func (a *agent) deleteInstance(t *testing.T, token, name string) {
	t.Helper()
	resp, body := a.api(http.MethodDelete, "/agent/v1/instances/"+name, token, nil)
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Logf("delete instance: status=%v body=%s", resp, body)
	}
}

// terminalEcho connects a WebSocket to the agent terminal, sends an echo
// command and waits for the marker to appear in the shell output.
func (a *agent) terminalEcho(t *testing.T, token, name, marker string) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(a.ts.URL, "http") + "/agent/v1/instances/" + name + "/terminal"
	hdr := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(u, hdr)
	if err != nil {
		t.Fatalf("terminal dial: %v", err)
	}
	defer conn.Close()
	_ = conn.WriteMessage(websocket.TextMessage, []byte("echo "+marker+"\n"))
	deadline := time.Now().Add(30 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("terminal read: %v (output so far: %q)", err, got.String())
		}
		got.Write(msg)
		if strings.Contains(got.String(), marker) {
			return
		}
	}
	t.Fatalf("terminal did not echo %q (output: %q)", marker, got.String())
}

// TestRebootAutoRestartAndTerminalReconnect is the end-to-end verification of
// the "host reboot" fix:
//
//  1. create an instance through the real agent API on the real engine
//  2. assert it runs and carries RestartPolicy=always (so the engine restarts
//     it at boot)
//  3. simulate a host reboot: power loss (container stopped), agent process
//     dies and boots again (same data dir), then the engine boot hook
//     (`podman start --all --filter restart-policy=always`, the same command
//     podman-restart.service runs on boot) brings containers back
//  4. assert the instance is running again
//  5. assert the WebSocket terminal reconnects and the shell works again
func TestRebootAutoRestartAndTerminalReconnect(t *testing.T) {
	sock := requireEngine(t)
	token := "e2e-token"
	name := fmt.Sprintf("e2e-reboot-%d", time.Now().UnixNano())
	dataDir := t.TempDir()
	t.Cleanup(func() { _ = runPodmanSilent("rm", "-f", name) })

	// --- Phase 1: agent boots, instance created ---
	a1 := bootAgent(t, dataDir, token, sock)
	a1.createInstance(t, token, name)

	deadline := time.Now().Add(60 * time.Second)
	for a1.instanceStatus(t, token, name) != "running" {
		if time.Now().After(deadline) {
			t.Fatalf("instance never became running")
		}
		time.Sleep(500 * time.Millisecond)
	}
	// The engine must carry the restart policy so it auto-starts the
	// container after a reboot (this is the root-cause fix).
	if got := runPodman(t, "inspect", "--format", "{{.HostConfig.RestartPolicy.Name}}", name); got != "always" {
		t.Fatalf("restart policy = %q, want always", got)
	}

	// Terminal works before the reboot.
	a1.terminalEcho(t, token, name, "E2E_BEFORE_REBOOT")

	// --- Phase 2: host reboot ---
	// Power loss: the container stops; the agent process dies too.
	runPodman(t, "stop", "-t0", name)
	a1.ts.Close() // agent process gone

	// Host boots: the engine's boot hook (podman-restart.service on real
	// hosts) starts every container whose policy is "always".
	runPodman(t, "start", "--all", "--filter", "restart-policy=always")

	// The agent comes back up on the same data dir and re-runs its boot
	// sequence (NAT rules rebuilt, restart policies re-ensured).
	a2 := bootAgent(t, dataDir, token, sock)

	// --- Phase 3: assert container auto-restarted + terminal reconnects ---
	deadline = time.Now().Add(60 * time.Second)
	for a2.instanceStatus(t, token, name) != "running" {
		if time.Now().After(deadline) {
			t.Fatalf("container did not auto-restart after simulated reboot (status=%q)", a2.instanceStatus(t, token, name))
		}
		time.Sleep(500 * time.Millisecond)
	}
	a2.terminalEcho(t, token, name, "E2E_AFTER_REBOOT")

	// Cleanup through the agent (removes the container and NAT rules).
	a2.deleteInstance(t, token, name)
}

func runPodmanSilent(args ...string) error {
	return exec.Command("podman", args...).Run()
}

// defaultIface returns the interface carrying the default route (used by the
// MASQUERADE rule); falls back to eth0 like the agent config default.
func defaultIface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err == nil {
		fields := strings.Fields(string(out))
		for i, f := range fields {
			if f == "dev" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return "eth0"
}
