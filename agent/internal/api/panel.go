package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/firewall"
)

// PanelHandler serves the local management web page at http://host:8792/.
// It lets an operator bind the node to the master by pasting the node token,
// without needing to edit files on the host.
type PanelHandler struct {
	cfg  config.Config
	tok  *TokenSource
	info InfoProvider
	fw   *firewall.Client
}

// InfoProvider lets the panel surface live runtime details.
type InfoProvider interface {
	InstanceCount() int
	Health() map[string]any
	// Diagnose returns a per-instance troubleshooting snapshot (IPv4
	// forwarding, firewall, applied NAT rules, container reachability, sshd).
	Diagnose(name string) map[string]any
}

func NewPanelHandler(cfg config.Config, tok *TokenSource, info InfoProvider) *PanelHandler {
	var fw *firewall.Client
	if strings.TrimSpace(cfg.RFWAddr) != "" {
		fw = firewall.New(cfg.RFWAddr)
	}
	return &PanelHandler{cfg: cfg, tok: tok, info: info, fw: fw}
}

type statusResp struct {
	Listen      string `json:"listen"`
	VirtType    string `json:"virt_type"`
	SocketPath  string `json:"socket_path"`
	WanIface    string `json:"wan_iface"`
	PortStart   int    `json:"port_start"`
	PortEnd     int    `json:"port_end"`
	DataDir     string `json:"data_dir"`
	MasterURL   string `json:"master_url"`
	TokenSet    bool   `json:"token_set"`
	HasPassword bool   `json:"has_password"`
	Instances   int    `json:"instances"`
	// IPv6 configuration incl. NDP responder status (subnet mode).
	IPv6Mode   string         `json:"ipv6_mode"`
	IPv6Addr   string         `json:"ipv6_addr"`
	IPv6Subnet string         `json:"ipv6_subnet"`
	NdpIface   string         `json:"ndp_iface"`
	NdpSubnets string         `json:"ndp_subnets"`
	Host       map[string]any `json:"host,omitempty"`
}

// Diag returns a troubleshooting snapshot for one instance: NAT rules,
// ip_forward, firewall status, container reachability and in-container sshd.
// Read-only and consistent with the panel's unauthenticated status view.
func (p *PanelHandler) Diag(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "实例名称不能为空"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": p.info.Diagnose(name)})
}

// Status returns the panel status JSON, including a live resource snapshot.
func (p *PanelHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, statusResp{
		Listen:      p.cfg.Listen,
		VirtType:    p.cfg.VirtType,
		SocketPath:  p.cfg.SocketPath,
		WanIface:    p.cfg.WanIface,
		PortStart:   p.cfg.PortStart,
		PortEnd:     p.cfg.PortEnd,
		DataDir:     p.cfg.DataDir,
		MasterURL:   p.cfg.MasterURL,
		TokenSet:    p.tok.Get() != "" && p.tok.Get() != "change-me",
		HasPassword: p.cfg.WebPassword != "",
		Instances:   p.info.InstanceCount(),
		IPv6Mode:    p.cfg.IPv6Mode,
		IPv6Addr:    p.cfg.IPv6Addr,
		IPv6Subnet:  p.cfg.IPv6Subnet,
		NdpIface:    p.cfg.NdpIface,
		NdpSubnets:  p.cfg.NdpSubnets,
		Host:        p.info.Health(),
	})
}

type settingsReq struct {
	WebPassword string `json:"web_password"`
	Token       string `json:"token"`
}

// Settings validates the web password and updates the node token.
func (p *PanelHandler) Settings(c *gin.Context) {
	var req settingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求体无效"})
		return
	}
	if p.cfg.WebPassword == "" {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "未设置面板密码（AGENT_WEB_PASSWORD），请通过 config.json 配置"})
		return
	}
	if req.WebPassword != p.cfg.WebPassword {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "面板密码错误"})
		return
	}
	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Token 不能为空"})
		return
	}
	if err := p.cfg.SetToken(req.Token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存配置失败: " + err.Error()})
		return
	}
	p.tok.Set(req.Token)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// Firewall returns rfw status + rule list for the local panel. Read-only, no
// password needed (mirrors the unauthenticated status view).
func (p *PanelHandler) Firewall(c *gin.Context) {
	if p.fw == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "rfw 未配置（RFW_ADDR 为空）"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	st, err := p.fw.Status(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	rules, err := p.fw.ListRules(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"status": st, "rules": rules}})
}

// FirewallCreate adds an rfw rule. Requires the panel password. The password
// travels inside the same JSON body as the rule, so the body is bound exactly
// once (gin consumes the request body, a second bind would always fail).
func (p *PanelHandler) FirewallCreate(c *gin.Context) {
	var req struct {
		firewall.CreateRule
		WebPassword string `json:"web_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求体无效"})
		return
	}
	if p.cfg.WebPassword == "" || req.WebPassword != p.cfg.WebPassword {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "面板密码错误或未配置"})
		return
	}
	if p.fw == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "rfw 未配置"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	rule, err := p.fw.CreateRule(ctx, req.CreateRule)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": rule})
}

// FirewallDelete removes an rfw rule. Requires the panel password.
func (p *PanelHandler) FirewallDelete(c *gin.Context) {
	if !p.authPanel(c) {
		return
	}
	if p.fw == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "rfw 未配置"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "规则 ID 无效"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := p.fw.DeleteRule(ctx, id); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
}

// authPanel verifies the panel password from the JSON body (web_password).
// Aborts the request with 403 when missing/mismatched.
func (p *PanelHandler) authPanel(c *gin.Context) bool {
	var req struct {
		WebPassword string `json:"web_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求体无效"})
		return false
	}
	if p.cfg.WebPassword == "" || req.WebPassword != p.cfg.WebPassword {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "面板密码错误或未配置"})
		return false
	}
	return true
}

// Update downloads the newest agent binary from the master, replaces the
// running executable and restarts the systemd unit. Requires the panel
// password; the install script's unit carries Restart=always.
func (p *PanelHandler) Update(c *gin.Context) {
	var req settingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求体无效"})
		return
	}
	if p.cfg.WebPassword == "" || req.WebPassword != p.cfg.WebPassword {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "面板密码错误或未配置"})
		return
	}
	if p.cfg.MasterURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "未配置 AGENT_MASTER_URL，无法自动更新"})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "无法定位程序路径"})
		return
	}
	url := strings.TrimRight(p.cfg.MasterURL, "/") + "/downloads/agent"
	tmp := exe + ".new"
	if err := downloadFile(url, tmp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "下载失败: " + err.Error()})
		return
	}
	_ = os.Chmod(tmp, 0o755)
	if err := os.Rename(tmp, exe); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "替换失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": nil})
	go func() {
		_ = exec.Command("systemctl", "restart", "codetest-agent").Run()
	}()
}

func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// Page renders the panel HTML.
func (p *PanelHandler) Page(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(c.Writer, panelHTML)
}

const panelHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Agent 管理面板</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; background:#09090b; color:#e2e8f0; font-family: ui-sans-serif, system-ui, sans-serif; }
  .wrap { max-width:620px; margin:0 auto; padding:32px 20px; }
  h1 { font-size:20px; margin:0 0 4px; }
  .sub { color:#64748b; font-size:13px; margin-bottom:24px; }
  .card { background:#121215; border:1px solid #27272a; border-radius:12px; padding:18px; margin-bottom:16px; }
  .card h2 { font-size:14px; margin:0 0 12px; color:#a5b4fc; }
  .row { display:flex; justify-content:space-between; font-size:13px; padding:5px 0; border-bottom:1px solid #1c1c1f; }
  .row:last-child { border-bottom:0; }
  .row span:first-child { color:#64748b; }
  .bar { height:8px; background:#1c1c1f; border-radius:99px; overflow:hidden; margin-top:6px; }
  .bar i { display:block; height:100%; background:#6366f1; border-radius:99px; transition:width .6s ease; }
  .bar.green i { background:#34d399; }
  .bar.amber i { background:#fbbf24; }
  .meter { padding:8px 0; border-bottom:1px solid #1c1c1f; }
  .meter:last-child { border-bottom:0; }
  .meter .lab { display:flex; justify-content:space-between; font-size:12px; color:#94a3b8; font-family:ui-monospace,monospace; }
  label { display:block; font-size:12px; color:#94a3b8; margin:12px 0 6px; }
  input { width:100%; background:#0b0b0e; border:1px solid #3f3f46; color:#e2e8f0; border-radius:8px; padding:9px 12px; font-size:14px; }
  input:focus { outline:2px solid #6366f1; border-color:transparent; }
  button { margin-top:16px; width:100%; background:#6366f1; border:0; color:#fff; font-size:14px; font-weight:600; border-radius:8px; padding:10px; cursor:pointer; }
  button:hover { background:#4f46e5; }
  button:disabled { opacity:.5; cursor:not-allowed; }
  .msg { margin-top:12px; font-size:13px; }
  .ok { color:#34d399; }
  .err { color:#f87171; }
  code { background:#0b0b0e; border:1px solid #3f3f46; border-radius:6px; padding:1px 6px; font-size:12px; }
  .pill { display:inline-block; padding:2px 8px; border-radius:999px; font-size:11px; background:#27272a; color:#a1a1aa; }
  .pill.on { background:#14532d; color:#4ade80; }
  .pill.off { background:#450a0a; color:#f87171; }
  .hint { font-size:11px; color:#64748b; margin-top:6px; }
  pre { background:#0b0b0e; border:1px solid #3f3f46; border-radius:8px; padding:10px; font-size:12px; overflow:auto; max-height:300px; color:#cbd5e1; margin-top:12px; }
</style>
</head>
<body>
<div class="wrap">
  <h1>Agent 管理面板</h1>
  <div class="sub">绑定节点 Token 后，服务端健康检查将自动把该机器标记为在线。资源数据每 5 秒刷新。</div>

  <div class="card">
    <h2>运行状态</h2>
    <div id="status"><p style="color:#64748b">加载中…</p></div>
    <div id="host"><p style="color:#64748b">资源数据加载中…</p></div>
  </div>

  <div class="card">
    <h2>绑定 Token</h2>
    <label>节点 Token（由服务端「节点管理」生成）</label>
    <input id="token" type="password" placeholder="粘贴节点 Token" autocomplete="off"/>
    <label>面板密码（AGENT_WEB_PASSWORD）</label>
    <input id="pw" type="password" placeholder="安装时生成的 Web 管理密码" autocomplete="current-password"/>
    <button id="save">保存并绑定</button>
    <div id="msg" class="msg"></div>
  </div>

  <div class="card">
    <h2>实例诊断</h2>
    <label>实例名称（诊断 SSH 无法连接问题：转发、防火墙、NAT、容器内 sshd）</label>
    <input id="dname" type="text" placeholder="如 narwhal-xxxx" autocomplete="off"/>
    <button id="diag">诊断</button>
    <pre id="dout" style="display:none"></pre>
  </div>

  <div class="card">
    <h2>防火墙 (rfw eBPF)</h2>
    <div id="fwstatus"><p style="color:#64748b">加载中…</p></div>
    <pre id="fwrules" style="display:none"></pre>
    <label>面板密码</label>
    <input id="fwpw" type="password" placeholder="Web 管理密码" autocomplete="current-password"/>
    <label>添加规则（JSON，可编辑）</label>
    <textarea id="fwjson" rows="4" style="background:#0b0b0e;border:1px solid #3f3f46;color:#e2e8f0;border-radius:8px;padding:9px 12px;font-size:12px;width:100%;font-family:ui-monospace,monospace">{"direction":"out","protocol":"tcp","port_start":25,"port_end":25,"ip_type":"any","action":"block"}</textarea>
    <div style="display:flex;gap:8px;margin-top:8px">
      <input id="fwdelid" type="number" placeholder="删除规则 ID" style="flex:1"/>
      <button id="fwdel" type="button" style="flex:0 0 auto;margin-top:0">删除</button>
      <button id="fwadd" type="button" style="flex:0 0 auto;margin-top:0">添加</button>
    </div>
    <div id="fwmsg" class="msg"></div>
    <div class="hint">GeoIP 拦截：ip_type=geoip, countries=["CN"]；CIDR：ip_type=cidr, ip="1.2.3.0/24"；协议：tcp/udp/http/tls/socks/ssh/fet/wireguard/openvpn/quic/all。</div>
  </div>

  <div class="card">
    <h2>检查更新</h2>
    <label>面板密码</label>
    <input id="upw" type="password" placeholder="Web 管理密码" autocomplete="current-password"/>
    <button id="update">从服务端拉取最新 Agent 并重启</button>
    <div id="umsg" class="msg"></div>
    <div class="hint">需服务端已上传新版本二进制（AGENT_BINARY_DIR）且本机已配置 AGENT_MASTER_URL。</div>
  </div>
</div>
<script>
function esc(s){return String(s==null?'':s).replace(/[&<>"]/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]})}
function fmt(n){if(!n&&n!==0)return '-';if(n<1024)return n+' B';if(n<1048576)return (n/1024).toFixed(1)+' KB';if(n<1073741824)return (n/1048576).toFixed(1)+' MB';return (n/1073741824).toFixed(2)+' GB'}
async function loadStatus() {
  try {
    const r = await fetch('/admin/status');
    const j = await r.json();
    const t = document.getElementById('status');
    const v6 = j.ipv6_mode ? (j.ipv6_mode === 'subnet'
      ? '<span class="pill on">subnet</span> NDP '+(j.ndp_iface?'<code>'+esc(j.ndp_iface)+'</code>':'—')
      : '<span class="pill on">snat</span>') : '<span class="pill off">none</span>';
    const rows = [
      ['监听地址', '<code>'+esc(j.listen)+'</code>'],
      ['虚拟化后端', esc(j.virt_type)],
      ['API Socket', '<code>'+esc(j.socket_path)+'</code>'],
      ['出口网卡', esc(j.wan_iface)],
      ['端口池', esc(j.port_start)+' – '+esc(j.port_end)],
      ['数据目录', '<code>'+esc(j.data_dir)+'</code>'],
      ['服务端地址', j.master_url ? '<code>'+esc(j.master_url)+'</code>' : '<span class="pill off">未配置</span>'],
      ['IPv6', v6],
      j.ipv6_subnet ? ['IPv6 子网', '<code>'+esc(j.ipv6_subnet)+'</code>'] : null,
      j.ndp_subnets ? ['NDP 代理子网', '<code>'+esc(j.ndp_subnets)+'</code>'] : null,
      ['实例数量', esc(j.instances)],
      ['Token', j.token_set ? '<span class="pill on">已配置</span>' : '<span class="pill off">未配置</span>']
    ].filter(Boolean);
    t.innerHTML = rows.map(([k,v]) => '<div class="row"><span>'+k+'</span><span>'+v+'</span></div>').join('');
    const h = j.host || {};
    if (h && h.host_cpu_percent !== undefined) {
      const cp = Number(h.host_cpu_percent||0);
      const mu = Number(h.host_mem_used_mb||0), mt = Number(h.host_mem_total_mb||1);
      const du = Number(h.host_disk_used_mb||0), dt = Number(h.host_disk_total_mb||1);
      const mp = mt?Math.min(100,mu/mt*100):0, dp = dt?Math.min(100,du/dt*100):0;
      document.getElementById('host').innerHTML =
        '<div class="meter"><div class="lab"><span>CPU</span><span>'+cp.toFixed(1)+'%</span></div><div class="bar"><i style="width:'+Math.min(100,cp)+'%"></i></div></div>'+
        '<div class="meter"><div class="lab"><span>内存</span><span>'+fmt(mu*1048576)+' / '+fmt(mt*1048576)+'</span></div><div class="bar green"><i style="width:'+mp+'%"></i></div></div>'+
        '<div class="meter"><div class="lab"><span>磁盘</span><span>'+fmt(du*1048576)+' / '+fmt(dt*1048576)+'</span></div><div class="bar amber"><i style="width:'+dp+'%"></i></div></div>'+
        '<div class="meter"><div class="lab"><span>负载</span><span>'+Number(h.load1||0).toFixed(2)+' / '+Number(h.load5||0).toFixed(2)+' / '+Number(h.load15||0).toFixed(2)+'</span></div></div>'+
        '<div class="meter"><div class="lab"><span>网络</span><span>↓ '+fmt(Number(h.net_in_bps||0))+'/s · ↑ '+fmt(Number(h.net_out_bps||0))+'/s</span></div></div>'+
        '<div class="meter"><div class="lab"><span>容器</span><span>'+esc(h.running_vms)+' / '+esc(h.total_vms)+' 运行</span></div></div>'+
        '<div class="meter"><div class="lab"><span>Uptime</span><span>'+esc(h.uptime||0)+'s</span></div></div>';
    }
  } catch(e) {
    document.getElementById('status').innerHTML = '<p class="err">加载失败</p>';
  }
}
document.getElementById('save').addEventListener('click', async () => {
  const msg = document.getElementById('msg');
  msg.className = 'msg';
  try {
    const r = await fetch('/admin/settings', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ web_password: document.getElementById('pw').value, token: document.getElementById('token').value })
    });
    const j = await r.json();
    if (j.code === 0) {
      msg.className = 'msg ok';
      msg.textContent = '绑定成功！服务端节点健康检查将自动将该机器标记为在线。';
      document.getElementById('token').value = '';
      document.getElementById('pw').value = '';
      loadStatus();
    } else {
      msg.className = 'msg err';
      msg.textContent = j.message || '保存失败';
    }
  } catch(e) { msg.className='msg err'; msg.textContent='请求失败'; }
});
document.getElementById('diag').addEventListener('click', async () => {
  const out = document.getElementById('dout');
  const name = document.getElementById('dname').value.trim();
  if (!name) { out.style.display='block'; out.textContent = '请输入实例名称'; return; }
  try {
    const r = await fetch('/admin/diag/' + encodeURIComponent(name));
    const j = await r.json();
    out.style.display = 'block';
    out.textContent = j.code === 0 ? JSON.stringify(j.data, null, 2) : (j.message || '诊断失败');
  } catch(e) { out.style.display='block'; out.textContent = '请求失败'; }
});
async function loadFirewall() {
  try {
    const r = await fetch('/admin/firewall');
    const j = await r.json();
    if (j.code !== 0) { document.getElementById('fwstatus').innerHTML = '<p class="err">'+esc(j.message||'加载失败')+'</p>'; return; }
    const st = j.data.status || {};
    document.getElementById('fwstatus').innerHTML =
      '<div class="row"><span>状态</span><span>'+(st.iface?'<span class="pill on">运行中</span> 网卡 '+esc(st.iface):'<span class="pill off">未运行</span>')+'</span></div>'+
      '<div class="row"><span>规则数量</span><span>'+esc(st.rule_count)+'</span></div>';
    const rules = j.data.rules || [];
    const pre = document.getElementById('fwrules');
    pre.style.display = rules.length ? 'block' : 'none';
    pre.textContent = rules.length ? rules.map(r => '['+r.id+'] '+(r.enabled?'ON':'OFF')+' '+r.action+' '+r.direction+' '+r.protocol+':'+r.port_start+(r.port_end!==r.port_start?'-'+r.port_end:'')+' '+r.ip_type+(r.ip?' '+r.ip:'')+(r.countries&&r.countries.length?' '+r.countries.join(','):'')).join('\n') : '';
  } catch(e) { document.getElementById('fwstatus').innerHTML = '<p class="err">加载失败</p>'; }
}
document.getElementById('fwadd').addEventListener('click', async () => {
  const msg = document.getElementById('fwmsg');
  msg.className = 'msg';
  try {
    const rule = JSON.parse(document.getElementById('fwjson').value);
    const r = await fetch('/admin/firewall', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.assign(rule, { web_password: document.getElementById('fwpw').value }))
    });
    const j = await r.json();
    if (j.code === 0) { msg.className='msg ok'; msg.textContent='规则已添加 (ID '+j.data.id+')'; loadFirewall(); }
    else { msg.className='msg err'; msg.textContent = j.message || '添加失败'; }
  } catch(e) { msg.className='msg err'; msg.textContent='JSON 解析失败或请求出错'; }
});
document.getElementById('fwdel').addEventListener('click', async () => {
  const msg = document.getElementById('fwmsg');
  msg.className = 'msg';
  const id = document.getElementById('fwdelid').value.trim();
  if (!id) { msg.className='msg err'; msg.textContent='请输入规则 ID'; return; }
  try {
    const r = await fetch('/admin/firewall/'+encodeURIComponent(id), {
      method: 'DELETE',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ web_password: document.getElementById('fwpw').value })
    });
    const j = await r.json();
    if (j.code === 0) { msg.className='msg ok'; msg.textContent='规则已删除'; document.getElementById('fwdelid').value=''; loadFirewall(); }
    else { msg.className='msg err'; msg.textContent = j.message || '删除失败'; }
  } catch(e) { msg.className='msg err'; msg.textContent='请求出错'; }
});
document.getElementById('update').addEventListener('click', async () => {
  const msg = document.getElementById('umsg');
  const btn = document.getElementById('update');
  msg.className = 'msg';
  btn.disabled = true;
  btn.textContent = '正在更新…';
  try {
    const r = await fetch('/admin/update', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ web_password: document.getElementById('upw').value })
    });
    const j = await r.json();
    if (j.code === 0) { msg.className='msg ok'; msg.textContent='已替换新版本，服务即将重启，请稍候重新打开本页。'; }
    else { msg.className='msg err'; msg.textContent = j.message || '更新失败'; }
  } catch(e) { msg.className='msg err'; msg.textContent='请求失败'; }
  btn.disabled = false;
  btn.textContent = '从服务端拉取最新 Agent 并重启';
});
loadStatus();
loadFirewall();
setInterval(loadStatus, 5000);
</script>
</body>
</html>`
