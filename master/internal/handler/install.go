package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/geo"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// imagePresets are the one-click OS images offered in the create-instance
// dialog. Their `ref` values are canonical docker.io/incus names; the agent
// install script configures a docker.io mirror so the OCI presets pull on
// hosts where docker.io is unreachable.
var imagePresets = []gin.H{
	{"id": "debian", "name": "Debian 13 (slim)", "ref": "debian:13-slim"},
	{"id": "alpine", "name": "Alpine Linux (latest)", "ref": "alpine:latest"},
	{"id": "ubuntu", "name": "Ubuntu 24.04 (LTS)", "ref": "ubuntu:24.04"},
}

type installScriptReq struct {
	WithToken   bool   `json:"with_token"`
	WebPassword string `json:"web_password"` // empty -> auto-generate
	VirtType    string `json:"virt_type"`    // incus | oci
	SocketPath  string `json:"socket_path"`  // empty -> backend default
	CLIBin      string `json:"cli_bin"`      // empty -> backend default
}

// ListImages returns the preset OS images for the create dialog.
func (h *Handler) ListImages(c *gin.Context) {
	response.OK(c, imagePresets)
}

// NodeInstallScript generates a one-shot bash installer for an existing node.
func (h *Handler) NodeInstallScript(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).First(&node, id).Error; err != nil {
		response.NotFound(c, "节点不存在")
		return
	}
	var req installScriptReq
	if !h.bind(c, &req) {
		return
	}
	script := h.buildInstallScript(c, node.Token, req.VirtType, req.WebPassword, req.WithToken, req.SocketPath, req.CLIBin)
	response.OK(c, gin.H{"script": script})
}

type addHostReq struct {
	Name        string `json:"name" binding:"required,min=1,max=64"`
	AgentAddr   string `json:"agent_addr"`   // 可留空：安装脚本装好后会自动回传
	Region      string `json:"region"`       // 可留空：按 IP 自动识别
	VirtType    string `json:"virt_type"`    // incus | oci
	WithToken   bool   `json:"with_token"`   // embed the node token in the script
	WebPassword string `json:"web_password"` // empty -> auto-generate
	SocketPath  string `json:"socket_path"`
	CLIBin      string `json:"cli_bin"`
}

// AddHost creates a node with a fresh token and returns the matching install
// script in one call — the streamlined onboarding flow used by the dashboard.
func (h *Handler) AddHost(c *gin.Context) {
	var req addHostReq
	if !h.bind(c, &req) {
		return
	}
	token := randHex(16)
	virtType := req.VirtType
	if virtType != "oci" {
		virtType = "incus"
	}
	node := model.Node{
		UserID:    auth.UID(c),
		Name:      req.Name,
		AgentAddr: normalizeAgentAddr(req.AgentAddr),
		Token:     token,
		Region:    req.Region,
		VirtType:  virtType,
		Status:    "offline",
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&node).Error; err != nil {
		response.BadRequest(c, "节点名称已存在")
		return
	}
	script := h.buildInstallScript(c, token, req.VirtType, req.WebPassword, req.WithToken, req.SocketPath, req.CLIBin)
	response.Created(c, gin.H{"node": node, "token": token, "script": script})
}

type registerNodeReq struct {
	Token     string `json:"token" binding:"required"`
	AgentAddr string `json:"agent_addr"` // reachable agent URL, may be empty
	Region    string `json:"region"`     // may be empty -> resolved from HostIP
	HostIP    string `json:"host_ip"`
	IPv6      string `json:"ipv6"`
}

// RegisterNode is called by the one-shot install script (or the agent panel)
// after the agent comes up. It lets nodes created without an agent_addr be
// discovered automatically: the host reports its own reachable address and IP,
// and the region is resolved via an IP geolocation lookup.
func (h *Handler) RegisterNode(c *gin.Context) {
	var req registerNodeReq
	if !h.bind(c, &req) {
		return
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).Where("token = ?", req.Token).First(&node).Error; err != nil {
		response.BadRequest(c, "Token 无效")
		return
	}
	updates := map[string]any{}
	if addr := strings.TrimSpace(req.AgentAddr); addr != "" {
		updates["agent_addr"] = normalizeAgentAddr(addr)
	}
	region := strings.TrimSpace(req.Region)
	if region == "" && strings.TrimSpace(req.HostIP) != "" {
		region = geo.Region(req.HostIP)
	}
	if region != "" {
		updates["region"] = region
	}
	if ip := strings.TrimSpace(req.HostIP); ip != "" {
		updates["host_ip"] = ip
	}
	if ip := strings.TrimSpace(req.IPv6); ip != "" {
		updates["ipv6"] = ip
	}
	if len(updates) == 0 {
		response.OK(c, nil)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(&node).Updates(updates).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	response.OK(c, nil)
}

// normalizeAgentAddr makes an agent address usable by the health client:
// - missing scheme gets http://
// - a bare IPv6 host (2001:db8::1) is wrapped in brackets
func normalizeAgentAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	if u, err := url.Parse(addr); err == nil {
		host := u.Host
		if host != "" && strings.Count(host, ":") >= 2 && !strings.HasPrefix(host, "[") {
			u.Host = "[" + host + "]"
			addr = u.String()
		}
	}
	return strings.TrimSuffix(addr, "/")
}

// buildInstallScript renders the one-shot installer for a node token.
func (h *Handler) buildInstallScript(c *gin.Context, token, virtType, webPassword string, withToken bool, socketPath, cliBin string) string {
	if virtType != "oci" {
		virtType = "incus"
	}
	masterURL := strings.TrimRight(h.cfg.PublicURL, "/")
	if masterURL == "" {
		masterURL = "http://" + c.Request.Host
	}
	embedToken := ""
	if withToken {
		embedToken = token
	}
	if socketPath == "" || cliBin == "" {
		if virtType == "oci" {
			if socketPath == "" {
				socketPath = "/var/run/docker.sock"
			}
			if cliBin == "" {
				cliBin = "docker"
			}
		} else {
			if socketPath == "" {
				socketPath = "/var/lib/incus/unix.socket"
			}
			if cliBin == "" {
				cliBin = "incus"
			}
		}
	}
	// 脚本本体托管于 GitHub（可 AGENT_INSTALL_SCRIPT_URL 覆盖）；生成的脚本只
	// 注入本节点参数后拉取执行，安装逻辑随仓库版本演进，无需重发 master。
	scriptURL := h.cfg.InstallScriptURL
	if scriptURL == "" {
		scriptURL = config.DefaultInstallScriptURL
	}

	var buf bytes.Buffer
	_ = installScriptTmpl.Execute(&buf, installScriptData{
		MasterURL:   masterURL,
		Token:       embedToken,
		HasToken:    withToken,
		WebPassword: webPassword,
		HasWebPass:  webPassword != "",
		VirtType:    virtType,
		SocketPath:  socketPath,
		CLIBin:      cliBin,
		ScriptURL:   scriptURL,
	})
	return buf.String()
}

// loadVerifyNdpScript returns the content of scripts/verify-ndp.sh so the
// install script can ship it to the node and auto-run the NDP self-check in
// subnet mode. The script is read from the explicitly configured path first,
// then from repo-relative locations (repo root, master dir, or the package dir
// when running under `go test`), then next to the agent binary. Returns ""
// when not found — the install script then skips the self-check gracefully.
func (h *Handler) loadVerifyNdpScript() string {
	candidates := []string{h.cfg.VerifyNdpScript}
	for _, p := range []string{
		"scripts/verify-ndp.sh",
		"../scripts/verify-ndp.sh",
		"../../scripts/verify-ndp.sh",
		"../../../scripts/verify-ndp.sh",
	} {
		candidates = append(candidates, p)
	}
	if h.cfg.AgentBinaryDir != "" {
		candidates = append(candidates, filepath.Join(h.cfg.AgentBinaryDir, "verify-ndp.sh"))
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return string(b)
		}
	}
	return ""
}

// DownloadAgent serves the agent binary so install scripts can fetch it from
// this master. It is intentionally public: the script runs on machines that
// have no credentials yet.
func (h *Handler) DownloadAgent(c *gin.Context) {
	path := filepath.Join(h.cfg.AgentBinaryDir, "agent")
	f, err := os.Open(path)
	if err != nil {
		response.NotFound(c, "未找到 Agent 二进制（请将其放到 "+path+" 或配置 AGENT_BINARY_DIR）")
		return
	}
	defer f.Close()
	c.Header("Content-Disposition", "attachment; filename=agent")
	http.ServeContent(c.Writer, c.Request, "agent", statTime(f), f)
}

type installScriptData struct {
	MasterURL   string
	Token       string
	HasToken    bool
	WebPassword string
	HasWebPass  bool
	VirtType    string
	SocketPath  string
	CLIBin      string
	ScriptURL   string
}

// installScriptTmpl generates a thin wrapper around the GitHub-hosted
// scripts/install-agent.sh: the master injects this node's parameters
// (MASTER_URL / VIRT_TYPE / token / web password / engine socket) as env vars,
// then the real installer is fetched and executed via the standard one-liner
// `bash <(curl -fsSL <script-url>)`. The installer itself is versioned
// in the repo, so updating agent install logic no longer requires redeploying
// the master binary.
var installScriptTmpl = template.Must(template.New("agent-install").Parse(`#!/usr/bin/env bash
# 独角鲸云 Agent 一键安装 / 增量更新（由服务端生成）
# 脚本本体托管于 GitHub：{{ .ScriptURL }}
# 用法：bash <(curl -fsSL ...) 或保存后 bash 该文件；可重复执行以升级 Agent。
set -euo pipefail

export MASTER_URL="{{ .MasterURL }}"
export VIRT_TYPE="{{ .VirtType }}"
export SOCKET_PATH="{{ .SocketPath }}"
export CLI_BIN="{{ .CLIBin }}"
{{ if .HasToken }}export TOKEN="{{ .Token }}"
{{ end }}{{ if .HasWebPass }}export WEB_PASSWORD="{{ .WebPassword }}"
{{ end }}
SCRIPT_URL="{{ .ScriptURL }}"
if command -v curl >/dev/null 2>&1; then
  bash <(curl -fsSL --max-time 120 "$SCRIPT_URL")
else
  echo "缺少 curl，无法拉取安装脚本"
  exit 1
fi
`))

func statTime(f *os.File) (t time.Time) {
	if fi, err := f.Stat(); err == nil {
		t = fi.ModTime()
	}
	return t
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
