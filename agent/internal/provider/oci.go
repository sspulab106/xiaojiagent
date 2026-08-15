package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ociProvider talks to a Docker-compatible engine API (Docker or Podman) over
// a unix socket. The engine API is a de-facto standard, so one client serves
// both engines; the agent config chooses the CLI used for exec terminals.
type ociProvider struct {
	hc    *http.Client
	quota quotaSupport

	// ipv6Mode/ipv6Subnet drive the IPv6 bridge network: "subnet" uses the
	// public container subnet with engine NAT disabled (addresses are routed,
	// not masqueraded — NDP is handled by the agent's responder); "snat" uses
	// a ULA subnet with engine MASQUERADE. "" / "none" stays IPv4-only.
	ipv6Mode   string
	ipv6Subnet string
}

func newOCI(socketPath string, opts Options) (*ociProvider, error) {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	mode := opts.IPv6Mode
	if mode == "none" {
		mode = ""
	}
	return &ociProvider{
		hc:         &http.Client{Transport: tr, Timeout: 120 * time.Second},
		ipv6Mode:   mode,
		ipv6Subnet: opts.IPv6Subnet,
	}, nil
}

const ociBase = "http://engine"

// net6Name identifies the IPv6-enabled bridge used by instances with IPv6
// enabled. In SNAT mode the subnet is a ULA range with egress via the host's
// ip6tables MASQUERADE (service.EnsureIPv6); in subnet mode it is a public
// sub-subnet routed to the host whose container addresses the agent
// NDP-proxies (agent/internal/ndp).
const net6Name = "narwhal-net6"
const net6Subnet = "fd00:10:91::/64"

func (p *ociProvider) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, ociBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("engine %s %s: %s", method, path, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// keepAliveCmd is the default command for OCI instances. Unlike LXD/Incus
// system containers, a bare OCI image (e.g. ubuntu) exits immediately when its
// default CMD finishes. To behave like a NAT VPS the container must stay up so
// users can exec in. sshd may be installed AFTER boot by the agent's SSH
// provisioning, so besides the initial attempt the loop also (re)starts sshd
// every 10s whenever it is not running. The loop additionally enforces the
// SSH config edits idempotently and restarts sshd when they change, so a
// stock openssh postinst config (PermitRootLogin prohibit-password on Debian)
// can never keep root password logins disabled.
var keepAliveCmd = []string{"/bin/sh", "-c",
	"apply_sshd() { [ -x /usr/sbin/sshd ] || return 0; sed -i 's/^#\\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config 2>/dev/null || true; sed -i 's/^#\\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config 2>/dev/null || true; command -v apk >/dev/null 2>&1 && sed -i '/^UsePAM/d' /etc/ssh/sshd_config 2>/dev/null || true; ssh-keygen -A >/dev/null 2>&1 || true; mkdir -p /run/sshd; if pgrep -x sshd >/dev/null 2>&1; then pkill -x sshd >/dev/null 2>&1 || true; sleep 1; fi; (service ssh start >/dev/null 2>&1 || service sshd start >/dev/null 2>&1 || /usr/sbin/sshd >/dev/null 2>&1) || true; if ! pgrep -x sshd >/dev/null 2>&1; then /usr/sbin/sshd >/dev/null 2>&1 || true; fi; }; trap 'exit 0' TERM; apply_sshd; while :; do if [ -x /usr/sbin/sshd ] && ! pgrep -x sshd >/dev/null 2>&1; then apply_sshd; fi; sleep 10 & wait $!; done"}

func (p *ociProvider) Create(ctx context.Context, spec Spec) (Info, error) {
	// Skip the pull when the image already exists locally (offline hosts,
	// locally-built images, repeat creates).
	if !p.imageExists(ctx, spec.Image) {
		if err := p.pullImage(ctx, spec.Image); err != nil {
			return Info{}, fmt.Errorf("pull image: %w", err)
		}
	}

	cmd := spec.Cmd
	if len(cmd) == 0 {
		cmd = keepAliveCmd
	}
	memoryBytes := spec.MemoryMB * 1024 * 1024
	// Docker/Podman MemorySwap is the TOTAL (memory+swap). Equal to Memory =>
	// no swap; memory+swap => swap sized as requested (defaults to = memory).
	memorySwap := memoryBytes
	if spec.SwapMB > 0 {
		memorySwap = (spec.MemoryMB + spec.SwapMB) * 1024 * 1024
	}
	hostConfig := map[string]any{
		"Memory":      memoryBytes,
		"MemorySwap":  memorySwap,
		"NanoCpus":    int64(spec.CpuCores) * 1_000_000_000,
		"NetworkMode": "bridge",
		// CAP_AUDIT_WRITE lets sshd allocate ptys inside the container
		// (its audit-netlink write fails with EPERM without it, which
		// aborts the session right after password auth).
		"CapAdd": []string{"AUDIT_WRITE"},
		// 宿主机整机重启后容器自动恢复（行为与真 VPS 一致）。引擎只在创建时
		// 应用重启策略，因此存量实例由 EnsureRestartPolicies 在 Agent 启动时补设。
		"RestartPolicy": map[string]any{"Name": "always"},
	}
	// Disk limit via the overlay project-quota storage option: the engine
	// enforces it by setting an XFS pquota / ext4 prjquota on the container's
	// diff dir, so `df -h /` inside the container reports the limit instead of
	// the host disk. Silently ignored on filesystems without project quotas.
	if spec.DiskMB > 0 && p.quota.supported(ctx, p) {
		hostConfig["StorageOpt"] = map[string]string{"size": fmt.Sprintf("%dM", spec.DiskMB)}
	}
	body := map[string]any{
		"Image":      spec.Image,
		"Cmd":        cmd,
		"Env":        []string{"TERM=xterm-256color"},
		"HostConfig": hostConfig,
	}
	// IPv6-enabled instances join the dedicated dual-stack bridge so they get
	// a ULA v6 address and (via SNAT) public IPv6 egress. If the network cannot
	// be prepared (engine without EnableIPv6 support, permission issues) the
	// instance is still created — it just runs without IPv6 rather than
	// blocking the whole create.
	if spec.IPv6 {
		if err := p.ensureNet6(ctx); err == nil {
			body["NetworkingConfig"] = map[string]any{
				"EndpointsConfig": map[string]any{net6Name: map[string]any{}},
			}
		}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := p.do(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(spec.Name), body, &created); err != nil {
		return Info{}, err
	}
	if err := p.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		return Info{}, err
	}
	return p.Get(ctx, spec.Name)
}

// ensureNet6 creates the IPv6-enabled bridge network once, if missing. The
// subnet comes from the config: the public container subnet in subnet mode
// (engine NAT disabled so addresses stay public) or the ULA range in snat
// mode (engine MASQUERADE provides egress; the agent's explicit rule in
// service.EnsureIPv6 covers engines where it is off).
func (p *ociProvider) ensureNet6(ctx context.Context) error {
	var nets []struct {
		Name string `json:"Name"`
	}
	if err := p.do(ctx, http.MethodGet, "/networks", nil, &nets); err != nil {
		return err
	}
	for _, n := range nets {
		if n.Name == net6Name {
			// Network already exists (e.g. a node upgraded from SNAT mode, or
			// created by an older agent build). Subnet mode still needs the
			// engine's v6 NAT switched off, so run the podman patch on this
			// path too; a stale subnet is repaired by reinstalling the node.
			if p.ipv6Mode == "subnet" {
				patchPodmanSnatV6(net6Name)
			}
			return nil
		}
	}
	subnet := p.ipv6Subnet
	if subnet == "" {
		subnet = net6Subnet
	}
	body := map[string]any{
		"Name":           net6Name,
		"Driver":         "bridge",
		"CheckDuplicate": true,
		"EnableIPv6":     true,
		"IPAM": map[string]any{
			"Config": []map[string]string{{"Subnet": subnet}},
		},
		"Options": map[string]string{"com.docker.network.bridge.name": "codetest6"},
	}
	if err := p.do(ctx, http.MethodPost, "/networks/create", body, nil); err != nil {
		return err
	}
	if p.ipv6Mode == "subnet" {
		// Public addresses must NOT be masqueraded. podman (netavark) reads
		// its snat_ipv6 flag from the network JSON — flip it here, exactly like
		// the reference installer's snat_ipv6=false injection. Note: the
		// docker-only com.docker.network.bridge.enable_ip_masquerade option is
		// deliberately NOT used — it would also kill IPv4 NAT on the same
		// network (and podman ignores it), so subnet mode is supported on
		// podman (which the install script installs); on docker it degrades to
		// SNAT-style v6 egress.
		patchPodmanSnatV6(net6Name)
	}
	return nil
}

// patchPodmanSnatV6 sets snat_ipv6=false on the podman network JSON so
// netavark does not masquerade the public container addresses. Best effort:
// podman-only, and only relevant in subnet mode.
func patchPodmanSnatV6(name string) {
	path := "/etc/containers/networks/" + name + ".json"
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return
	}
	opts, _ := cfg["options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
	}
	if opts["snat_ipv6"] == "false" {
		return
	}
	opts["snat_ipv6"] = "false"
	cfg["options"] = opts
	out, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, out, 0o644)
}

// pullImage streams the docker image pull; progress JSON is discarded.
func (p *ociProvider) pullImage(ctx context.Context, image string) error {
	return p.do(ctx, http.MethodPost, "/images/create?fromImage="+url.QueryEscape(image), nil, nil)
}

// imageExists reports whether the image reference is already present locally.
func (p *ociProvider) imageExists(ctx context.Context, image string) bool {
	path := "/images/" + url.PathEscape(image) + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ociBase+path, nil)
	if err != nil {
		return false
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (p *ociProvider) Start(ctx context.Context, name string) error {
	id, err := p.containerID(ctx, name)
	if err != nil {
		return err
	}
	return p.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil)
}

func (p *ociProvider) Stop(ctx context.Context, name string, force bool) error {
	id, err := p.containerID(ctx, name)
	if err != nil {
		return err
	}
	if force {
		return p.do(ctx, http.MethodPost, "/containers/"+id+"/kill", nil, nil)
	}
	return p.do(ctx, http.MethodPost, "/containers/"+id+"/stop", nil, nil)
}

func (p *ociProvider) Restart(ctx context.Context, name string) error {
	id, err := p.containerID(ctx, name)
	if err != nil {
		return err
	}
	return p.do(ctx, http.MethodPost, "/containers/"+id+"/restart", nil, nil)
}

func (p *ociProvider) Delete(ctx context.Context, name string) error {
	// The engine resolves container names for DELETE, so no containerID lookup
	// is needed. A 404 ("no such container") means it is already gone — treat
	// it as success so the agent can still clean up its NAT rules and state
	// (e.g. when someone removed the container behind the agent's back).
	err := p.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?force=true", nil, nil)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "404") || strings.Contains(msg, "no such container") {
			return nil
		}
		return err
	}
	return nil
}

// Rebuild for OCI is delete-then-create with the same name. The engine reuses
// the free IP, so existing NAT rules pointing at that IP keep working.
func (p *ociProvider) Rebuild(ctx context.Context, name string, spec Spec) error {
	// Spec must carry the full limits; the service restores them from state.
	if err := p.Delete(ctx, name); err != nil {
		return err
	}
	_, err := p.Create(ctx, spec)
	return err
}

func (p *ociProvider) Get(ctx context.Context, name string) (Info, error) {
	id, err := p.containerID(ctx, name)
	if err != nil {
		return Info{}, err
	}
	var md struct {
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress         string `json:"IPAddress"`
				GlobalIPv6Address string `json:"GlobalIPv6Address"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := p.do(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &md); err != nil {
		return Info{}, err
	}
	info := Info{Name: name, Status: normalizeStatus(md.State.Status)}
	for _, nw := range md.NetworkSettings.Networks {
		if nw.IPAddress != "" {
			info.IP = nw.IPAddress
			break
		}
	}
	if v6 := md.NetworkSettings.Networks[net6Name].GlobalIPv6Address; v6 != "" {
		info.IPv6 = v6
	}
	return info, nil
}

func (p *ociProvider) List(ctx context.Context) ([]Info, error) {
	var items []struct {
		ID              string   `json:"Id"`
		Names           []string `json:"Names"`
		State           string   `json:"State"`
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress         string `json:"IPAddress"`
				GlobalIPv6Address string `json:"GlobalIPv6Address"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := p.do(ctx, http.MethodGet, "/containers/json?all=1", nil, &items); err != nil {
		return nil, err
	}
	res := make([]Info, 0, len(items))
	for _, it := range items {
		info := Info{Name: strings.TrimPrefix(firstOrEmpty(it.Names), "/"), Status: normalizeStatus(it.State)}
		for _, nw := range it.NetworkSettings.Networks {
			if nw.IPAddress != "" {
				info.IP = nw.IPAddress
				break
			}
		}
		if v6 := it.NetworkSettings.Networks[net6Name].GlobalIPv6Address; v6 != "" {
			info.IPv6 = v6
		}
		res = append(res, info)
	}
	return res, nil
}

func (p *ociProvider) Stats(ctx context.Context, name string) (Stats, error) {
	id, err := p.containerID(ctx, name)
	if err != nil {
		return Stats{}, err
	}
	info, err := p.Get(ctx, name)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{Status: info.Status}

	var s struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
		} `json:"memory_stats"`
		Networks map[string]struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"networks"`
	}
	if err := p.do(ctx, http.MethodGet, "/containers/"+id+"/stats?stream=false", nil, &s); err != nil {
		return Stats{}, err
	}

	cpuDelta := s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage
	sysDelta := s.CPUStats.SystemCPUUsage - s.PreCPUStats.SystemCPUUsage
	if sysDelta > 0 {
		ncpus := s.CPUStats.OnlineCPUs
		if ncpus == 0 {
			ncpus = 1
		}
		st.CPUPercent = float64(cpuDelta) / float64(sysDelta) * float64(ncpus) * 100
	}
	st.MemoryUsedMB = int64(s.MemoryStats.Usage / (1024 * 1024))
	st.MemoryLimitMB = int64(s.MemoryStats.Limit / (1024 * 1024))
	for _, nw := range s.Networks {
		st.RxBytes += nw.RxBytes
		st.TxBytes += nw.TxBytes
	}
	return st, nil
}

// EnsureRestartPolicies sets RestartPolicy=always on every existing container.
// The engine only applies the restart policy at create time, so containers
// created before that was introduced need a one-time reconcile on agent
// startup — otherwise a host reboot leaves them stopped. Best effort: a
// container that fails to update is skipped so one bad apple never blocks the
// rest (e.g. podman versions without container-update support).
func (p *ociProvider) EnsureRestartPolicies(ctx context.Context) error {
	var items []struct {
		ID string `json:"Id"`
	}
	if err := p.do(ctx, http.MethodGet, "/containers/json?all=1", nil, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := p.do(ctx, http.MethodPost, "/containers/"+it.ID+"/update", map[string]any{
			"RestartPolicy": map[string]string{"Name": "always"},
		}, nil); err != nil {
			continue
		}
	}
	return nil
}

// containerID resolves a container name or ID to its ID via the inspect
// endpoint. Using the name directly in the URL usually works too, but this is
// more robust and keeps one lookup path for every operation.
func (p *ociProvider) containerID(ctx context.Context, name string) (string, error) {
	var md struct {
		ID string `json:"Id"`
	}
	if err := p.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil, &md); err != nil {
		return "", err
	}
	if md.ID == "" {
		return "", fmt.Errorf("container %q not found", name)
	}
	return md.ID, nil
}

// normalizeStatus maps engine-specific container states to the platform's
// canonical set: running | stopped | paused.
func normalizeStatus(s string) string {
	switch strings.ToLower(s) {
	case "running":
		return "running"
	case "paused":
		return "paused"
	case "exited", "stopped", "configured", "created", "dead":
		return "stopped"
	default:
		return strings.ToLower(s)
	}
}

func firstOrEmpty(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}
