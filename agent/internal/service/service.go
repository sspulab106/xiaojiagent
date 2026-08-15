package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/firewall"
	"example.com/codetest/agent/internal/nat"
	"example.com/codetest/agent/internal/ndp"
	"example.com/codetest/agent/internal/provider"
)

// Service orchestrates the virtualization provider, NAT manager, firewall
// proxy and state. The REST handlers in this package delegate here.
type Service struct {
	cfg        config.Config
	prov       provider.Provider
	nat        *nat.Manager
	state      *State
	fw         *firewall.Client
	ndp        *ndp.Manager
	selfcheck  *SelfCheckManager
	log        *slog.Logger
	netSampler *netSampler

	// fwCache short-circuits FirewallInfo so a down rfw daemon never adds
	// up to ~1.2s of latency to every health poll (panel 5s, master 30s).
	fwMu    sync.Mutex
	fwInfo  map[string]any
	fwInfoT time.Time
}

func New(cfg config.Config, prov provider.Provider, log *slog.Logger) *Service {
	state := loadState(cfg.DataDir)
	return &Service{
		cfg:   cfg,
		prov:  prov,
		nat:   nat.NewManager(cfg.WanIface, state.Rules),
		state: state,
		fw:    newFirewallClient(cfg.RFWAddr),
		// The NDP responder only runs in subnet mode with a configured WAN
		// interface and container subnets (see agent/internal/ndp).
		ndp:        ndp.New(cfg.NdpIface, cfg.NdpSubnets, log),
		selfcheck:  NewSelfCheckManager(cfg.VerifyNdpScript, cfg.DataDir, localBaseURL(cfg.Listen), log),
		log:        log,
		netSampler: newNetSampler(),
	}
}

// EnsureIPv6 applies the host-level IPv6 settings required for container IPv6
// egress (SNAT mode): forwarding sysctls and an ip6tables MASQUERADE rule for
// the container ULA subnet out of the WAN interface. Call once at startup.
func (s *Service) EnsureIPv6() error {
	if s.cfg.IPv6Mode == "" || s.cfg.IPv6Mode == "none" {
		return nil
	}
	// IPv6 forwarding + RA acceptance mirror the reference install: with
	// forwarding enabled the kernel drops router advertisements by default,
	// which would silently kill the host's own default IPv6 route.
	iface := s.cfg.IPv6Iface
	if iface == "" {
		iface = s.cfg.WanIface
	}
	conf := "net.ipv6.conf.all.forwarding=1\n" +
		"net.ipv6.conf.default.forwarding=1\n" +
		"net.ipv6.conf.all.accept_ra=2\n" +
		"net.ipv6.conf.default.accept_ra=2\n" +
		"net.ipv6.conf.all.use_tempaddr=0\n" +
		"net.ipv6.conf.default.use_tempaddr=0\n"
	_ = os.WriteFile("/etc/sysctl.d/99-codetest-ipv6.conf", []byte(conf), 0o644)
	for _, kv := range strings.Split(strings.TrimSpace(conf), "\n") {
		_ = runSysctl(kv)
	}

	if s.cfg.IPv6Mode == "snat" && iface != "" {
		subnet := s.cfg.IPv6Subnet
		if subnet == "" {
			subnet = "fd00:10:91::/64"
		}
		// Belt-and-suspenders: engines that enable ip6tables for their bridge
		// set this themselves; the explicit rule makes SNAT work even when the
		// engine's own IPv6 NAT is off.
		if err := ensureIP6Rule("ip6tables", []string{"-t", "nat", "-A", "POSTROUTING", "-s", subnet, "-o", iface, "-j", "MASQUERADE"}); err != nil {
			s.log.Warn("enable ipv6 SNAT", "subnet", subnet, "iface", iface, "err", err)
		} else {
			s.log.Info("ipv6 SNAT enabled", "mode", s.cfg.IPv6Mode, "subnet", subnet, "iface", iface)
		}
	}
	if s.cfg.IPv6Mode == "subnet" {
		// Subnet mode: containers hold public addresses from a sub-subnet of
		// the host's /64, so the host must act as the NDP responder. Mirror the
		// reference installer's host routing setup, then proxy container
		// addresses out of the WAN interface.
		s.configureSubnetRouting(iface, s.cfg.IPv6Addr)
		if s.ndp != nil {
			if err := s.ndp.EnableProxy(); err != nil {
				s.log.Warn("enable ndp proxy", "err", err)
			} else {
				s.log.Info("ndp responder enabled", "iface", s.ndp.Iface(), "subnets", s.ndp.Subnets())
			}
			s.syncNDP(context.Background())
		}
	}
	return nil
}

// configureSubnetRouting pins the host's public IPv6 to /128 and disables
// SLAAC address autoconfiguration on the WAN interface, mirroring the
// reference installer's configure_host_ipv6_routing(). With forwarding on,
// accept_ra=2 keeps the default route; autoconf=0/accept_ra_pinfo=0 stop the
// kernel from minting new EUI-64/private addresses. Pinning to /128 removes
// the /64 connected route on the WAN interface so it can never shadow the
// container sub-subnet route on the bridge. Best-effort and idempotent.
func (s *Service) configureSubnetRouting(iface, addr string) {
	if iface == "" {
		return
	}
	for key, val := range map[string]string{
		"net.ipv6.conf." + iface + ".autoconf":        "0",
		"net.ipv6.conf." + iface + ".accept_ra_pinfo": "0",
		"net.ipv6.conf." + iface + ".accept_ra":       "2",
	} {
		if err := runSysctl(key + "=" + val); err != nil {
			s.log.Warn("ipv6 subnet sysctl", "key", key, "err", err)
		}
	}
	if addr == "" {
		return
	}
	// Pin the host address to /128, then drop SLAAC-generated dynamic global
	// addresses (their /64 connected route conflicts with the container /112).
	out, err := exec.Command("ip", "-6", "addr", "show", "dev", iface, "scope", "global").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		cur := fields[1]
		base := strings.SplitN(cur, "/", 2)[0]
		if base == addr && !strings.HasSuffix(cur, "/128") {
			if err := exec.Command("ip", "-6", "addr", "del", cur, "dev", iface).Run(); err != nil {
				s.log.Warn("del host ipv6 /64", "addr", cur, "err", err)
				continue
			}
			if err := exec.Command("ip", "-6", "addr", "add", addr+"/128", "dev", iface).Run(); err != nil {
				s.log.Warn("add host ipv6 /128", "addr", addr, "err", err)
			}
		}
		if base != addr {
			for _, f := range fields[2:] {
				if f == "dynamic" {
					if err := exec.Command("ip", "-6", "addr", "del", cur, "dev", iface).Run(); err != nil {
						s.log.Warn("del dynamic ipv6", "addr", cur, "err", err)
					}
					break
				}
			}
		}
	}
}

// syncNDP reconciles the NDP proxy table with the live container IPv6
// addresses (adds new, drops stale). Used at startup, after create, and after
// start/restart/rebuild where the engine may reassign addresses.
func (s *Service) syncNDP(ctx context.Context) {
	if s.ndp == nil || !s.ndp.Enabled() {
		return
	}
	infos, err := s.prov.List(ctx)
	if err != nil {
		s.log.Warn("ndp sync", "err", err)
		return
	}
	addrs := make([]string, 0, len(infos))
	for _, in := range infos {
		ipv6 := in.IPv6
		// Some backends (incus) don't include addresses in List — fetch them
		// individually so startup and action syncs still proxy every
		// container's public address.
		if ipv6 == "" {
			if g, gerr := s.prov.Get(ctx, in.Name); gerr == nil {
				ipv6 = g.IPv6
			}
		}
		if ipv6 != "" {
			addrs = append(addrs, ipv6)
		}
	}
	s.ndp.Sync(addrs)
}

func runSysctl(kv string) error {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	return exec.Command("sysctl", "-w", parts[0]+"="+parts[1]).Run()
}

// ensureIP6Rule is like nat.ensureRule but for ip6tables: idempotent add.
func ensureIP6Rule(bin string, args []string) error {
	check := make([]string, 0, len(args))
	for _, a := range args {
		if a == "-A" {
			check = append(check, "-C")
		} else {
			check = append(check, a)
		}
	}
	if err := exec.Command(bin, check...).Run(); err == nil {
		return nil // already present
	}
	return exec.Command(bin, args...).Run()
}

// RebuildNAT re-applies all persisted rules; call once at startup.
func (s *Service) RebuildNAT() error {
	// NAT'd container traffic is silently dropped when IPv4 forwarding is off
	// (clients see "connection timed out"), so enforce it on every boot.
	if err := s.nat.EnsureForwarding(); err != nil {
		s.log.Warn("enable ip_forward", "err", err)
	}
	// podman/docker can reassign container IPs across restarts, so reconcile
	// the persisted rules against the live containers first, then re-apply.
	if err := s.syncRuleIPs(context.Background()); err != nil {
		s.log.Warn("sync NAT rule IPs", "err", err)
	}
	// Drop leftover DNAT rules from previous agent builds whose container IPs
	// no longer match state, so a stale rule can never shadow a fresh one.
	s.nat.PurgeStale(s.cfg.PortStart, s.cfg.PortEnd)
	return s.nat.RebuildAll()
}

// syncRuleIPs reconciles persisted NAT rules with the live container IPs.
// Called at startup and after a container start/restart (both can move a
// container onto a new IP), so DNAT targets never point at stale addresses.
func (s *Service) syncRuleIPs(ctx context.Context) error {
	infos, err := s.prov.List(ctx)
	if err != nil {
		return err
	}
	ipByName := make(map[string]string, len(infos))
	for _, in := range infos {
		ipByName[in.Name] = in.IP
	}
	for _, r := range s.state.RulesCopy() {
		ip, ok := ipByName[r.InstanceName]
		if !ok || ip == "" || ip == r.ContainerIP {
			continue
		}
		s.log.Info("container IP changed, retargeting NAT", "instance", r.InstanceName, "old", r.ContainerIP, "new", ip)
		updated, err := s.nat.UpdateIP(r.ID, ip)
		if err != nil {
			s.log.Warn("update NAT rule IP", "rule", r.ID, "err", err)
			continue
		}
		s.state.UpdateRule(updated)
	}
	return nil
}

// CreateRequest is the instance creation payload from the master.
type CreateRequest struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	CpuCores int    `json:"cpu_cores"`
	MemoryMB int64  `json:"memory_mb"`
	DiskMB   int64  `json:"disk_mb"`
	SwapMB   int64  `json:"swap_mb"`
	IPv6     bool   `json:"ipv6"`
}

// CreateResponse mirrors the master's expectations.
type CreateResponse struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	IP           string `json:"ip"`
	SSHPort      int    `json:"ssh_port"`
	SSHRuleID    string `json:"ssh_rule_id"`   // NAT rule id so the master can delete it later
	RootPassword string `json:"root_password"` // generated & set in-container
}

func (s *Service) CreateInstance(ctx context.Context, req CreateRequest) (CreateResponse, error) {
	spec := provider.Spec{
		Name:     req.Name,
		Image:    req.Image,
		CpuCores: req.CpuCores,
		MemoryMB: req.MemoryMB,
		DiskMB:   req.DiskMB,
		SwapMB:   req.SwapMB,
		IPv6:     req.IPv6,
	}
	info, err := s.prov.Create(ctx, spec)
	if err != nil {
		return CreateResponse{}, fmt.Errorf("创建实例失败: %w", err)
	}
	s.state.SetInstance(spec)
	// Subnet mode: the container just got a public IPv6 address; make sure the
	// NDP responder advertises it (address assignment is usually immediate,
	// and syncNDP re-runs on later start/restart anyway).
	if s.ndp != nil && info.IPv6 != "" {
		if err := s.ndp.Add(info.IPv6); err != nil {
			s.log.Warn("ndp proxy add", "instance", spec.Name, "addr", info.IPv6, "err", err)
		}
	}

	// Auto-allocate an SSH mapping (container port 22 -> host port).
	hostPort, err := s.nat.AllocatePort(0, s.cfg.PortStart, s.cfg.PortEnd, "tcp")
	if err != nil {
		return CreateResponse{}, fmt.Errorf("分配 SSH 端口失败: %w", err)
	}
	rule := nat.Rule{
		ID:            uuid.NewString(),
		InstanceName:  spec.Name,
		HostPort:      hostPort,
		ContainerIP:   info.IP,
		ContainerPort: 22,
		Protocol:      "tcp",
		CreatedAt:     time.Now(),
	}
	if err := s.nat.Add(rule); err != nil {
		return CreateResponse{}, fmt.Errorf("配置 NAT 失败: %w", err)
	}
	s.state.AddRule(rule)

	// One-click SSH: install/start sshd inside the container and set a root
	// password. Runs ASYNC so instance creation returns immediately (image
	// pulls / apt/apk provisioning can take minutes, especially on slow
	// mirrors); the container itself is already running and NAT is in place.
	// The keep-alive loop restarts sshd every 10s once it is installed, so
	// SSH comes up automatically once provisioning finishes.
	rootPassword := randHex(8)
	go func() {
		pctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := s.provisionSSH(pctx, spec.Name, rootPassword); err != nil {
			s.log.Warn("ssh provisioning", "instance", spec.Name, "err", err)
		} else {
			s.log.Info("ssh provisioning done", "instance", spec.Name)
		}
	}()

	return CreateResponse{Name: info.Name, Status: info.Status, IP: info.IP, SSHPort: hostPort, SSHRuleID: rule.ID, RootPassword: rootPassword}, nil
}

// provisionSSH installs openssh-server (apt/apk) when missing, sets the root
// password and starts sshd inside the container via the provider CLI.
// Best-effort: failures leave the container usable but without sshd.
func (s *Service) provisionSSH(ctx context.Context, name, password string) error {
	script := `# disable IPv6 to dodge AAAA-first connection hangs (best effort)
echo 1 > /proc/sys/net/ipv6/conf/all/disable_ipv6 2>/dev/null || true
# install sshd only when the image lacks it; skip on prebuilt sshd images
if ! command -v sshd >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    # openssh-server postinst may start sshd BEFORE we edit sshd_config, and
    # passwd(1) provides chpasswd which minimal images (debian-slim) lack.
    (apt-get -o Acquire::ForceIPv4=true update -qq && apt-get -o Acquire::ForceIPv4=true install -y -qq openssh-server passwd) >/dev/null 2>&1 || true
  elif command -v apk >/dev/null 2>&1; then
    # alpine package repos default to https; on networks where large TLS
    # transfers stall (e.g. WSL2 NAT) apk can hang forever. Packages are
    # RSA-signed so http mirrors are safe.
    sed -i 's#^https://#http://#' /etc/apk/repositories >/dev/null 2>&1 || true
    (apk add --no-cache openssh) >/dev/null 2>&1 || true
  fi
fi
echo 'root:` + password + `' | chpasswd >/dev/null 2>&1 || true
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config >/dev/null 2>&1 || true
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config >/dev/null 2>&1 || true
# PAM session modules (pam_loginuid/pam_keyinit) fail inside containers and make
# sshd close the connection right after password auth ("closed by remote host").
# NOTE: alpine's sshd is compiled without PAM and treats any UsePAM directive as
# a FATAL config error ("Unsupported option UsePAM"), so never add it there.
if command -v apk >/dev/null 2>&1; then
  sed -i '/^UsePAM/d' /etc/ssh/sshd_config >/dev/null 2>&1 || true
else
  sed -i 's/^#\?UsePAM.*/UsePAM no/' /etc/ssh/sshd_config >/dev/null 2>&1 || true
fi
# Generate host keys when missing (alpine's openssh does not ship/auto-generate
# them; sshd refuses to start without them). No-op on debian where they exist.
ssh-keygen -A >/dev/null 2>&1 || true
mkdir -p /run/sshd
# CRITICAL: the container's keep-alive loop (or the openssh postinst) may have
# already started sshd with the stock config, where Debian defaults to
# "PermitRootLogin prohibit-password" and refuses root password logins. The
# config edits above only take effect on a fresh start, so force a restart.
pkill -x sshd >/dev/null 2>&1 || true
sleep 1
if command -v service >/dev/null 2>&1; then
  (service ssh start || service sshd start || true) >/dev/null 2>&1 || true
fi
(/usr/sbin/sshd || true) >/dev/null 2>&1 || true
`
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.CLIBin, "exec", name, "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("provision ssh: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Action maps a lifecycle verb to the provider. Rebuild can switch to a new
// image and re-provision SSH; the stored spec keeps the limits.
func (s *Service) Action(ctx context.Context, name, action, image, password string) (provider.Info, error) {
	var err error
	switch action {
	case "start":
		err = s.prov.Start(ctx, name)
	case "stop":
		err = s.prov.Stop(ctx, name, true)
	case "restart":
		err = s.prov.Restart(ctx, name)
	case "rebuild":
		spec, ok := s.state.GetInstance(name)
		if !ok {
			return provider.Info{}, errors.New("实例规格未知，无法重建")
		}
		if image != "" {
			spec.Image = image
			s.state.SetInstance(spec)
		}
		err = s.prov.Rebuild(ctx, name, spec)
		if err == nil && password != "" {
			// 换镜像重装后原容器内的 sshd/密码都没了，后台重新预置。
			pctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			go func() {
				if perr := s.provisionSSH(pctx, name, password); perr != nil {
					s.log.Warn("rebuild ssh provisioning", "instance", name, "err", perr)
				}
			}()
		}
	default:
		return provider.Info{}, fmt.Errorf("未知操作: %s", action)
	}
	if err != nil {
		return provider.Info{}, err
	}
	// start/restart/rebuild can land the container on a new IP (v4 or v6);
	// retarget any persisted NAT rules and re-sync NDP proxies so the exposed
	// port and the public IPv6 keep working.
	if action == "start" || action == "restart" || action == "rebuild" {
		if err := s.syncRuleIPs(ctx); err != nil {
			s.log.Warn("sync NAT rule IPs", "instance", name, "err", err)
		}
		s.syncNDP(ctx)
	}
	return s.prov.Get(ctx, name)
}

func (s *Service) DeleteInstance(ctx context.Context, name string) error {
	// Remove NAT rules first so no dangling DNAT targets remain. The removed
	// rules are also dropped from persisted state so a restart doesn't rebuild
	// them.
	removed := s.nat.RemoveByInstance(name)
	for _, r := range removed {
		s.state.RemoveRule(r.ID)
	}
	// Drop the NDP proxy entry for the instance's public IPv6 (best effort).
	if info, err := s.prov.Get(ctx, name); err == nil && s.ndp != nil && info.IPv6 != "" {
		s.ndp.Del(info.IPv6)
	}
	if err := s.prov.Delete(ctx, name); err != nil {
		return fmt.Errorf("删除实例失败: %w", err)
	}
	s.state.RemoveInstance(name)
	// Reconcile the NDP proxy table so entries for externally-removed
	// containers (Get above may have errored) are dropped promptly.
	s.syncNDP(ctx)
	return nil
}

// AddPortRequest creates one NAT mapping. HostPort == 0 means auto-allocate.
type AddPortRequest struct {
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
	HostPort      int    `json:"host_port"`
}

func (s *Service) AddPort(ctx context.Context, name string, req AddPortRequest) (nat.Rule, error) {
	if req.Protocol == "" {
		req.Protocol = "tcp"
	}
	if req.Protocol != "tcp" && req.Protocol != "udp" {
		return nat.Rule{}, fmt.Errorf("协议仅支持 tcp/udp")
	}
	info, err := s.prov.Get(ctx, name)
	if err != nil {
		return nat.Rule{}, err
	}
	if info.IP == "" {
		return nat.Rule{}, errors.New("无法获取容器 IP")
	}
	hostPort, err := s.nat.AllocatePort(req.HostPort, s.cfg.PortStart, s.cfg.PortEnd, req.Protocol)
	if err != nil {
		return nat.Rule{}, err
	}
	rule := nat.Rule{
		ID:            uuid.NewString(),
		InstanceName:  name,
		HostPort:      hostPort,
		ContainerIP:   info.IP,
		ContainerPort: req.ContainerPort,
		Protocol:      req.Protocol,
		CreatedAt:     time.Now(),
	}
	if err := s.nat.Add(rule); err != nil {
		return nat.Rule{}, err
	}
	s.state.AddRule(rule)
	return rule, nil
}

func (s *Service) RemovePort(ruleID string) error {
	if err := s.nat.Remove(ruleID); err != nil {
		return err
	}
	s.state.RemoveRule(ruleID)
	return nil
}

func (s *Service) List(ctx context.Context) ([]provider.Info, error) {
	return s.prov.List(ctx)
}

func (s *Service) Get(ctx context.Context, name string) (provider.Info, error) {
	return s.prov.Get(ctx, name)
}

func (s *Service) Stats(ctx context.Context, name string) (provider.Stats, error) {
	return s.prov.Stats(ctx, name)
}

// InstanceCount returns the number of instances this agent tracks (used by
// the management panel status page).
func (s *Service) InstanceCount() int {
	return len(s.state.Instances)
}

// ChangePassword updates the root password inside a running container. The
// password is piped via stdin to `chpasswd` to avoid command-line injection.
func (s *Service) ChangePassword(ctx context.Context, name, password string) error {
	if len(password) < 6 || len(password) > 64 {
		return errors.New("密码长度需在 6-64 位之间")
	}
	script := "chpasswd 2>/dev/null || true\npasswd -u root >/dev/null 2>&1 || true\n"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.CLIBin, "exec", "-i", name, "sh", "-c", script)
	cmd.Stdin = strings.NewReader("root:" + password + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("修改密码失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Diagnose collects a per-instance snapshot used by the management panel to
// pinpoint why an instance is unreachable from outside. It surfaces the four
// classic failure points in one place: IPv4 forwarding, firewall status, the
// applied NAT rules, container reachability and in-container sshd state.
func (s *Service) Diagnose(name string) map[string]any {
	// Bound the whole snapshot so a wedged container can never hang the
	// management panel's diag endpoint.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := s.prov.Get(ctx, name)
	if err != nil {
		info = provider.Info{Name: name, Status: "unknown"}
	}
	var rules []nat.Rule
	for _, r := range s.state.RulesCopy() {
		if r.InstanceName == name {
			rules = append(rules, r)
		}
	}
	forward, _ := exec.Command("sysctl", "-n", "net.ipv4.ip_forward").Output()

	fws := map[string]string{}
	if out, err := exec.Command("ufw", "status").Output(); err == nil {
		fws["ufw"] = "active"
		if !strings.Contains(string(out), "Status: active") {
			fws["ufw"] = "inactive"
		}
	}
	if out, err := exec.Command("systemctl", "is-active", "firewalld").Output(); err == nil {
		fws["firewalld"] = strings.TrimSpace(string(out))
	}

	// Whether the intended DNAT / FORWARD rules are actually installed.
	dnatApplied, fwdApplied := false, false
	if len(rules) > 0 {
		r := rules[0]
		dnatApplied = exec.Command("iptables", "-t", "nat", "-C", "PREROUTING",
			"-p", r.Protocol, "--dport", fmt.Sprint(r.HostPort),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort)).Run() == nil
	}
	if info.IP != "" {
		fwdApplied = exec.Command("iptables", "-C", "FORWARD", "-d", info.IP+"/32", "-j", "ACCEPT").Run() == nil
	}

	ping := "n/a"
	if info.IP != "" {
		if err := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", info.IP).Run(); err == nil {
			ping = "ok"
		} else {
			ping = "fail"
		}
	}

	sshd := "unknown"
	if out, err := exec.CommandContext(ctx, s.cfg.CLIBin, "exec", name, "sh", "-c",
		"if command -v sshd >/dev/null 2>&1; then if pgrep -x sshd >/dev/null 2>&1 || pidof sshd >/dev/null 2>&1; then echo running; else echo installed-not-running; fi; else echo missing; fi").Output(); err == nil {
		sshd = strings.TrimSpace(string(out))
	}

	return map[string]any{
		"instance":          info,
		"nat_rules":         rules,
		"ip_forward":        strings.TrimSpace(string(forward)),
		"firewall":          fws,
		"dnat_applied":      dnatApplied,
		"forward_applied":   fwdApplied,
		"container_ping":    ping,
		"sshd_in_container": sshd,
	}
}

// TerminalCommand returns the CLI command that opens a shell inside the
// container. It execs bash when the image has it and falls back to sh
// (busybox/dash) otherwise — alpine images don't ship bash.
func (s *Service) TerminalCommand(name string) []string {
	shellCmd := "command -v bash >/dev/null 2>&1 && exec bash || exec sh"
	switch s.cfg.VirtType {
	case "oci":
		// -i is essential: without it podman allocates a tty but never attaches
		// stdin, so the Web terminal receives output yet all input is dropped.
		return []string{s.cfg.CLIBin, "exec", "-ti", name, "/bin/sh", "-c", shellCmd}
	default:
		return []string{s.cfg.CLIBin, "exec", name, "/bin/sh", "-c", shellCmd}
	}
}

// Normalize trims names to avoid injection-style surprises in URLs/commands.
func Normalize(name string) string {
	return strings.TrimSpace(name)
}
