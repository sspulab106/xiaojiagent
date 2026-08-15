package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/config"
)

// validatePythonDict parses a Python dict literal via the python3 binary when
// available; otherwise falls back to a structural (balanced-brace) check so the
// test still runs on machines without python3. The defaults dict references an
// env() helper, which is stubbed here.
func validatePythonDict(literal string) error {
	if !strings.HasPrefix(strings.TrimSpace(literal), "{") || !strings.HasSuffix(strings.TrimSpace(literal), "}") {
		return fmt.Errorf("not a dict literal")
	}
	if strings.Count(literal, "{") != strings.Count(literal, "}") {
		return fmt.Errorf("unbalanced braces")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return nil // no python3: structural check above is enough
	}
	src := "def env(k, default=''): return default\nd = " + literal + "\n"
	r, err := exec.Command("python3", "-c", src).CombinedOutput()
	if err != nil {
		return fmt.Errorf("python parse failed: %v: %s", err, r)
	}
	return nil
}

// TestInstallScriptRender verifies the generated install script is a thin
// wrapper around the GitHub-hosted scripts/install-agent.sh: it injects the
// node's parameters as env vars and executes the canonical one-liner
// `bash <(curl -fsSL <script-url>)`.
func TestInstallScriptRender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	h := &Handler{cfg: config.Config{PublicURL: "http://master.example.com"}}
	s := h.buildInstallScript(c, "tok123", "oci", "pw456", true, "", "")
	for _, want := range []string{
		"#!/usr/bin/env bash", "set -euo pipefail",
		`export MASTER_URL="http://master.example.com"`,
		`export VIRT_TYPE="oci"`,
		`export SOCKET_PATH="/var/run/docker.sock"`,
		`export CLI_BIN="docker"`,
		`export TOKEN="tok123"`,
		`export WEB_PASSWORD="pw456"`,
		"bash <(curl -fsSL --max-time 120 \"$SCRIPT_URL\")",
		"raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("install wrapper missing %q", want)
		}
	}
	// 未勾选 token / 未填密码时不导出对应变量（由安装脚本保留既有值）。
	s2 := h.buildInstallScript(c, "", "incus", "", false, "", "")
	for _, notWant := range []string{"export TOKEN=", "export WEB_PASSWORD="} {
		if strings.Contains(s2, notWant) {
			t.Fatalf("wrapper must not export %q when not provided", notWant)
		}
	}
	for _, want := range []string{`export VIRT_TYPE="incus"`, `export SOCKET_PATH="/var/lib/incus/unix.socket"`, `export CLI_BIN="incus"`} {
		if !strings.Contains(s2, want) {
			t.Fatalf("wrapper missing %q", want)
		}
	}
	// 自定义 ScriptURL 生效。
	h3 := &Handler{cfg: config.Config{PublicURL: "http://master.example.com", InstallScriptURL: "https://example.com/install.sh"}}
	s3 := h3.buildInstallScript(c, "tok123", "oci", "pw456", true, "", "")
	if !strings.Contains(s3, "https://example.com/install.sh") {
		t.Fatalf("custom InstallScriptURL not used")
	}
}

// readRepoInstallScript loads the canonical GitHub-hosted installer from the
// repo (scripts/install-agent.sh) — tests exercise the real file, not a copy.
func readRepoInstallScript(t *testing.T) string {
	t.Helper()
	paths := []string{
		filepath.Join("..", "..", "..", "scripts", "install-agent.sh"), // 从 master/internal/handler 出发
		filepath.Join("scripts", "install-agent.sh"),
		filepath.Join("master", "..", "scripts", "install-agent.sh"),
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b)
		}
	}
	t.Fatalf("scripts/install-agent.sh not found (tried %v)", paths)
	return ""
}

// runConfigMerge extracts the python config-merge block from the installer
// script and executes it against the given data dir, feeding runtime values
// through the same env vars the script exports.
func runConfigMerge(t *testing.T, script, dataDir string) map[string]any {
	t.Helper()
	start := strings.Index(script, "python3 - <<'PYEOF'")
	end := strings.Index(script, "PYEOF\nchmod 600")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("python merge block not found")
	}
	// 提取块以 `python3 - <<'PYEOF'` 开头，去掉首行，余下即 Python 源码。
	block := script[start:end]
	block = block[strings.Index(block, "\n")+1:]
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	cmd := exec.Command("python3", "-")
	cmd.Stdin = strings.NewReader(block)
	cmd.Env = append(os.Environ(),
		"CFG_DATA_DIR="+dataDir,
		"CFG_LISTEN=:8792",
		"CFG_VIRT_TYPE=oci",
		"CFG_SOCKET_PATH=/var/run/docker.sock",
		"CFG_CLI_BIN=docker",
		"CFG_WEB_PASSWORD_DEFAULT=auto-gen-pass",
		"CFG_PORT_START=20000",
		"CFG_PORT_END=40000",
		"CFG_WAN_IFACE=eth0",
		"CFG_MASTER_URL=http://new-master.example.com",
		"CFG_RFW_ADDR=127.0.0.1:7734",
		"CFG_IPV6_MODE=snat",
		"CFG_IPV6_ADDR=",
		"CFG_IPV6_SUBNET=fd00:10:91::/64",
		"CFG_IPV6_IFACE=eth0",
		"CFG_NDP_IFACE=",
		"CFG_NDP_SUBNETS=",
		"CFG_NDP_NETWORK=",
		"TOKEN=",
		"WEB_PASSWORD=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("merge block failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("merged config not valid JSON: %v\n%s", err, b)
	}
	return m
}

// TestInstallScriptMergePreservesMethod verifies that re-running the install
// script to update an existing agent keeps the original install method
// (virt_type / socket_path / cli_bin / listen / port range) and credentials
// (token / web_password), while refreshing runtime-detected values like
// master_url and preserving unknown keys from newer agent versions.
func TestInstallScriptMergePreservesMethod(t *testing.T) {
	script := readRepoInstallScript(t)

	// 脚本本体含增量合并说明与保留键集合。
	for _, want := range []string{
		"增量合并配置（保留安装方式与 token/密码，不覆盖已有机器状态）",
		"method_keys = {", "defaults = {", "provided = {}",
		"if k not in provided and k in old:",
		"listen", "virt_type", "data_dir", "port_start", "port_end",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-agent.sh missing %q", want)
		}
	}

	dir := t.TempDir()
	old := map[string]any{
		"listen":       ":9999",
		"virt_type":    "incus",
		"data_dir":     "/opt/custom-agent",
		"socket_path":  "/run/incus.sock",
		"cli_bin":      "incus",
		"port_start":   30000,
		"port_end":     50000,
		"token":        "old-token-keep",
		"web_password": "old-pass-keep",
		"master_url":   "http://old-master.example.com",
		"ipv6_mode":    "subnet",
		"future_key":   "keep-me",
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	m := runConfigMerge(t, script, dir)
	checks := map[string]string{
		// 安装方式与凭据：保留原值
		"listen":       ":9999",
		"virt_type":    "incus",
		"data_dir":     "/opt/custom-agent",
		"socket_path":  "/run/incus.sock",
		"cli_bin":      "incus",
		"port_start":   "30000",
		"port_end":     "50000",
		"token":        "old-token-keep",
		"web_password": "old-pass-keep",
		// 运行时探测键：以本次为准
		"master_url": "http://new-master.example.com",
		// 未知键：原样保留
		"future_key": "keep-me",
	}
	for k, want := range checks {
		got := fmt.Sprintf("%v", m[k])
		if got != want {
			t.Fatalf("merged %s = %v, want %q", k, m[k], want)
		}
	}
	if _, ok := m["ipv6_mode"]; !ok {
		t.Fatalf("runtime keys missing: %v", m)
	}
}

// TestInstallScriptHasPortCheck verifies the installer includes port-occupancy
// detection: it must hard-fail when the listen port is taken by a foreign
// process, allow it when owned by the previous agent (incremental update), and
// fail when the whole container port range is exhausted.
func TestInstallScriptHasPortCheck(t *testing.T) {
	script := readRepoInstallScript(t)
	for _, want := range []string{
		"端口占用检测",
		"LISTEN_PORT=\"${LISTEN##*:}\"",
		"已被其他进程占用",
		"codetest-agent",
		"端口段",
		"PORT_START=\"${PORT_START:-20000}\"",
		"PORT_END=\"${PORT_END:-40000}\"",
		"exit 1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-agent.sh missing port-check marker %q", want)
		}
	}
	// 端口段必须可被环境变量覆盖并传入配置合并。
	if !strings.Contains(script, `CFG_PORT_START="${PORT_START}"`) || !strings.Contains(script, `CFG_PORT_END="${PORT_END}"`) {
		t.Fatalf("port range env overrides not wired into config merge")
	}
}

// TestInstallScriptMergeFirstInstall verifies a fresh install (no existing
// config) writes the defaults, including an auto-generated web password.
func TestInstallScriptMergeFirstInstall(t *testing.T) {
	script := readRepoInstallScript(t)
	dir := t.TempDir()
	m := runConfigMerge(t, script, dir)
	for k, want := range map[string]string{
		"listen":       ":8792",
		"virt_type":    "oci",
		"data_dir":     dir,
		"master_url":   "http://new-master.example.com",
		"port_start":   "20000",
		"port_end":     "40000",
		"web_password": "auto-gen-pass",
	} {
		if got := fmt.Sprintf("%v", m[k]); got != want {
			t.Fatalf("first install %s = %v, want %q", k, m[k], want)
		}
	}
}

// TestInstallScriptMergeExplicitOverrides verifies that explicitly provided
// TOKEN / WEB_PASSWORD override the preserved values (admin regenerated them).
func TestInstallScriptMergeExplicitOverrides(t *testing.T) {
	script := readRepoInstallScript(t)
	dir := t.TempDir()
	old := map[string]any{"token": "old-token", "web_password": "old-pass", "virt_type": "oci"}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	start := strings.Index(script, "python3 - <<'PYEOF'")
	end := strings.Index(script, "PYEOF\nchmod 600")
	block := script[start:end]
	block = block[strings.Index(block, "\n")+1:]
	cmd := exec.Command("python3", "-")
	cmd.Stdin = strings.NewReader(block)
	cmd.Env = append(os.Environ(),
		"CFG_DATA_DIR="+dir, "CFG_LISTEN=:8792", "CFG_VIRT_TYPE=oci",
		"CFG_SOCKET_PATH=/var/run/docker.sock", "CFG_CLI_BIN=docker",
		"CFG_WEB_PASSWORD_DEFAULT=auto-gen-pass", "CFG_PORT_START=20000", "CFG_PORT_END=40000",
		"CFG_WAN_IFACE=eth0", "CFG_MASTER_URL=http://new-master.example.com",
		"CFG_RFW_ADDR=127.0.0.1:7734", "CFG_IPV6_MODE=snat", "CFG_IPV6_ADDR=",
		"CFG_IPV6_SUBNET=fd00:10:91::/64", "CFG_IPV6_IFACE=eth0",
		"CFG_NDP_IFACE=", "CFG_NDP_SUBNETS=", "CFG_NDP_NETWORK=",
		"TOKEN=new-token", "WEB_PASSWORD=new-pass",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("merge failed: %v\n%s", err, out)
	}
	m := map[string]any{}
	bb, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	_ = json.Unmarshal(bb, &m)
	if m["token"] != "new-token" || m["web_password"] != "new-pass" {
		t.Fatalf("explicit overrides not applied: %+v", m)
	}
}
