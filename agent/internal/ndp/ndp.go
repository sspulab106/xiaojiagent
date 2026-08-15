// Package ndp implements the agent's built-in NDP responder used by IPv6
// subnet mode. Containers get public addresses from a sub-subnet of the
// host's /64 (e.g. 2001:db8::cafe:0/112). The upstream router routes the
// whole /64 to the host, so it resolves container addresses by sending
// neighbor solicitations to the host's WAN MAC. The kernel only answers NS
// for addresses that are local or present in the per-interface proxy table,
// so the agent must install `ip -6 neigh add proxy <addr> dev <wan>` entries
// as containers come and go. This mirrors the reference installer's "NDP
// responder handled by the Agent" design without external daemons.
package ndp

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Manager proxies neighbor solicitations for container IPv6 addresses out of
// a WAN interface. A nil *Manager (or a Manager with Enabled() == false)
// disables all NDP handling.
type Manager struct {
	iface   string
	subnets []*net.IPNet

	mu      sync.Mutex
	proxied map[string]bool // addresses we have installed a proxy entry for
	log     *slog.Logger
}

// New builds a Manager for the given WAN interface and comma-separated
// subnets (e.g. "2001:db8::cafe:0/112,2001:db8::d00d:0/112"). It returns a
// disabled manager when there is nothing to proxy.
func New(iface, subnets string, log *slog.Logger) *Manager {
	m := &Manager{
		iface:   strings.TrimSpace(iface),
		proxied: map[string]bool{},
		log:     log,
	}
	for _, s := range strings.Split(subnets, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			if log != nil {
				log.Warn("invalid ndp subnet, ignoring", "subnet", s, "err", err)
			}
			continue
		}
		m.subnets = append(m.subnets, n)
	}
	if m.iface == "" || len(m.subnets) == 0 {
		return m // disabled
	}
	return m
}

// Enabled reports whether this manager will actually install proxy entries.
func (m *Manager) Enabled() bool {
	return m != nil && m.iface != "" && len(m.subnets) > 0
}

// Iface returns the WAN interface proxies are installed on.
func (m *Manager) Iface() string {
	if m == nil {
		return ""
	}
	return m.iface
}

// Subnets returns the proxy subnets as CIDR strings.
func (m *Manager) Subnets() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.subnets))
	for _, n := range m.subnets {
		out = append(out, n.String())
	}
	return out
}

// EnableProxy turns on kernel proxy_ndp for the WAN interface and persists it
// so the responder survives reboots.
func (m *Manager) EnableProxy() error {
	if !m.Enabled() {
		return fmt.Errorf("ndp responder disabled")
	}
	key := "net.ipv6.conf." + m.iface + ".proxy_ndp"
	if err := run("sysctl", "-w", key+"=1"); err != nil {
		return fmt.Errorf("enable proxy_ndp: %w", err)
	}
	// Persist alongside the other agent IPv6 sysctls.
	line := key + "=1\n"
	if b, err := os.ReadFile("/etc/sysctl.d/99-codetest-ipv6.conf"); err == nil {
		if strings.Contains(string(b), key) {
			return nil
		}
		line = string(b) + line
	}
	return os.WriteFile("/etc/sysctl.d/99-codetest-ipv6.conf", []byte(line), 0o644)
}

// Add installs a proxy entry for one container address. Addresses outside the
// configured subnets are rejected so a stray address can never redirect NS
// handling. Idempotent.
func (m *Manager) Add(addr string) error {
	if !m.Enabled() {
		return fmt.Errorf("ndp responder disabled")
	}
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return fmt.Errorf("invalid ipv6 address %q", addr)
	}
	if ip.To4() != nil {
		return fmt.Errorf("ndp proxy is ipv6-only: %q", addr)
	}
	if !m.contains(ip) {
		return fmt.Errorf("address %s is outside ndp subnets %s", ip, strings.Join(m.Subnets(), ","))
	}
	// Run the kernel command outside the lock: exec is slow and the tracked
	// set is only consulted under the mutex. Concurrent Adds of the same
	// address are safe (addProxy tolerates "File exists").
	if err := m.addProxy(ip.String()); err != nil {
		return err
	}
	m.mu.Lock()
	m.proxied[ip.String()] = true
	m.mu.Unlock()
	return nil
}

// Del removes the proxy entry for an address (best effort — the kernel drops
// entries when the neighbor ages out or the container vanishes).
func (m *Manager) Del(addr string) {
	if !m.Enabled() {
		return
	}
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.proxied, ip.String())
	_ = exec.Command("ip", "-6", "neigh", "del", "proxy", ip.String(), "dev", m.iface).Run()
}

// Sync reconciles the proxy table with the live set of container addresses:
// new addresses get entries, previously proxied addresses that are no longer
// present are removed. Returns the number of entries now installed.
func (m *Manager) Sync(addrs []string) int {
	if !m.Enabled() {
		return 0
	}
	seen := map[string]bool{}
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		ip := net.ParseIP(a)
		if ip == nil || ip.To4() != nil || !m.contains(ip) {
			continue
		}
		seen[ip.String()] = true
		if err := m.Add(ip.String()); err != nil && m.log != nil {
			m.log.Warn("ndp proxy add", "addr", ip.String(), "err", err)
		}
	}
	// Collect stale entries under the lock, then exec outside it.
	m.mu.Lock()
	var stale []string
	for addr := range m.proxied {
		if !seen[addr] {
			delete(m.proxied, addr)
			stale = append(stale, addr)
		}
	}
	count := len(m.proxied)
	m.mu.Unlock()
	for _, addr := range stale {
		if err := exec.Command("ip", "-6", "neigh", "del", "proxy", addr, "dev", m.iface).Run(); err != nil && m.log != nil {
			m.log.Debug("ndp proxy del", "addr", addr, "err", err)
		}
	}
	return count
}

// Proxied returns the currently installed proxy addresses (sorted for stable
// output in the panel).
func (m *Manager) Proxied() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.proxied))
	for a := range m.proxied {
		out = append(out, a)
	}
	// Stable ordering for display.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Info returns a panel/health snapshot of the responder.
func (m *Manager) Info() map[string]any {
	if m == nil || !m.Enabled() {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"enabled":   true,
		"iface":     m.iface,
		"subnets":   m.Subnets(),
		"proxied":   m.Proxied(),
		"proxied_n": len(m.Proxied()),
	}
}

func (m *Manager) contains(ip net.IP) bool {
	for _, n := range m.subnets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (m *Manager) addProxy(addr string) error {
	err := exec.Command("ip", "-6", "neigh", "add", "proxy", addr, "dev", m.iface).Run()
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "file exists") {
		return nil // already present — fine
	}
	return err
}

func run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
