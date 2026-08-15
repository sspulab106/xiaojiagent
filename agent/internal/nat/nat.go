// Package nat manages iptables DNAT/MASQUERADE rules that expose container
// internal ports on the host's public address. Rules are persisted so they can
// be re-applied on agent restart.
package nat

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rule is one NAT mapping.
type Rule struct {
	ID            string    `json:"id"`
	InstanceName  string    `json:"instance_name"`
	HostPort      int       `json:"host_port"`
	ContainerIP   string    `json:"container_ip"`
	ContainerPort int       `json:"container_port"`
	Protocol      string    `json:"protocol"` // tcp | udp
	CreatedAt     time.Time `json:"created_at"`
}

// Manager applies and tracks NAT rules in memory. Callers persist the rule
// list elsewhere (see the agent service State).
type Manager struct {
	mu    sync.Mutex
	wan   string
	rules []Rule
}

func NewManager(wanIface string, rules []Rule) *Manager {
	return &Manager{wan: wanIface, rules: rules}
}

// Rules returns a copy of the current rule list.
func (m *Manager) Rules() []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Rule, len(m.rules))
	copy(cp, m.rules)
	return cp
}

// EnsureForwarding enables IPv4 forwarding on the host and persists the
// setting. When ip_forward is off the kernel silently drops packets destined
// to containers, so external clients see "connection timed out" instead of a
// working SSH session — the #1 failure on freshly installed nodes. Called
// before every rule application so it also self-heals hosts where the setting
// was lost (reboot, firewall manager reset).
func (m *Manager) EnsureForwarding() error {
	if err := run("sysctl", []string{"-w", "net.ipv4.ip_forward=1"}); err != nil {
		return err
	}
	_ = os.WriteFile("/etc/sysctl.d/99-codetest-nat.conf", []byte("net.ipv4.ip_forward=1\n"), 0o644)
	return nil
}

// Add applies a rule via iptables, then records it.
func (m *Manager) Add(r Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.EnsureForwarding(); err != nil {
		return err
	}
	if err := m.applyRule(r); err != nil {
		return err
	}
	m.rules = append(m.rules, r)
	return nil
}

// Remove deletes a rule by ID, applying the iptables removal first.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.ID == id {
			if err := m.removeRule(r); err != nil {
				return err
			}
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

// RemoveByInstance deletes all rules belonging to an instance and returns the
// removed rules so callers can drop them from persisted state.
func (m *Manager) RemoveByInstance(name string) []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept, removed []Rule
	for _, r := range m.rules {
		if r.InstanceName == name {
			_ = m.removeRule(r)
			removed = append(removed, r)
			continue
		}
		kept = append(kept, r)
	}
	m.rules = kept
	return removed
}

// UpdateIP retargets an existing rule to a new container IP (podman/docker can
// assign a different IP when a container restarts, leaving the old DNAT target
// dangling). It removes the stale iptables rules and applies the new ones,
// returning the updated rule so the caller can persist it.
func (m *Manager) UpdateIP(id, newIP string) (Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rules {
		if m.rules[i].ID == id {
			old := m.rules[i]
			if old.ContainerIP == newIP {
				return old, nil
			}
			_ = m.removeRule(old)
			m.rules[i].ContainerIP = newIP
			if err := m.applyRule(m.rules[i]); err != nil {
				return Rule{}, err
			}
			return m.rules[i], nil
		}
	}
	return Rule{}, fmt.Errorf("rule %s not found", id)
}

// RebuildAll re-applies every rule; called once at agent startup.
func (m *Manager) RebuildAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if err := m.applyRule(r); err != nil {
			return err
		}
	}
	return nil
}

// PurgeStale removes DNAT rules in the nat table whose host port falls inside
// the agent's port pool but whose target differs from the active rule set.
// Leftovers from a previous agent build (e.g. old container IPs after a
// rebuild) would otherwise shadow the fresh rules, since iptables matches the
// FIRST rule for a given dport. Rules outside the pool (other services) and
// rules that match the active set are left untouched.
func (m *Manager) PurgeStale(portStart, portEnd int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := make(map[string]string) // "proto:dport" -> "ip:port"
	for _, r := range m.rules {
		active[fmt.Sprintf("%s:%d", r.Protocol, r.HostPort)] = fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort)
	}
	for _, chain := range []string{"PREROUTING", "OUTPUT"} {
		out, err := exec.Command("iptables", "-t", "nat", "-S", chain).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[0] != "-A" {
				continue
			}
			proto, dport, target := "", "", ""
			for i := 2; i < len(fields); i++ {
				switch fields[i] {
				case "-p":
					if i+1 < len(fields) {
						proto = fields[i+1]
					}
				case "--dport":
					if i+1 < len(fields) {
						dport = fields[i+1]
					}
				case "--to-destination":
					if i+1 < len(fields) {
						target = fields[i+1]
					}
				}
			}
			if proto == "" || dport == "" || target == "" {
				continue
			}
			p, err := strconv.Atoi(dport)
			if err != nil || p < portStart || p > portEnd {
				continue
			}
			if active[fmt.Sprintf("%s:%d", proto, p)] == target {
				continue // matches an active rule
			}
			del := append([]string{"-t", "nat", "-D", fields[1]}, fields[2:]...)
			_ = run("iptables", del)
		}
	}
}

func (m *Manager) applyRule(r Rule) error {
	// DNAT in PREROUTING covers external traffic (the normal NAT VPS path).
	dnatPre := []string{
		"-t", "nat", "-A", "PREROUTING", "-p", r.Protocol, "--dport", fmt.Sprint(r.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort),
	}
	if err := ensureRule("iptables", dnatPre); err != nil {
		return fmt.Errorf("DNAT %d: %w", r.HostPort, err)
	}
	// DNAT in OUTPUT covers connections originating on the host itself
	// (loopback/hairpin), so `ssh root@<hostip> -p <port>` works locally too.
	dnatOut := []string{
		"-t", "nat", "-A", "OUTPUT", "-p", r.Protocol, "--dport", fmt.Sprint(r.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort),
	}
	if err := ensureRule("iptables", dnatOut); err != nil {
		return fmt.Errorf("DNAT-OUT %d: %w", r.HostPort, err)
	}
	mask := []string{
		"-t", "nat", "-A", "POSTROUTING", "-s", r.ContainerIP + "/32", "-o", m.wan, "-j", "MASQUERADE",
	}
	if err := ensureRule("iptables", mask); err != nil {
		return fmt.Errorf("MASQUERADE %s: %w", r.ContainerIP, err)
	}
	// The host FORWARD chain defaults to DROP; podman's CNI rules only accept
	// container-initiated and established traffic, so inbound NEW connections
	// (external clients / Windows host) would be dropped. Accept both
	// directions for the container explicitly. The rules are INSERTED at the
	// top of the chain (not appended): on hosts with ufw/firewalld a DROP or
	// REJECT rule earlier in the chain (or the chain policy) would otherwise
	// shadow them and inbound traffic would time out.
	fwdTo := []string{"-I", "FORWARD", "1", "-d", r.ContainerIP + "/32", "-j", "ACCEPT"}
	if err := ensureRule("iptables", fwdTo); err != nil {
		return fmt.Errorf("FORWARD-to %s: %w", r.ContainerIP, err)
	}
	fwdFrom := []string{"-I", "FORWARD", "1", "-s", r.ContainerIP + "/32", "-j", "ACCEPT"}
	if err := ensureRule("iptables", fwdFrom); err != nil {
		return fmt.Errorf("FORWARD-from %s: %w", r.ContainerIP, err)
	}
	return nil
}

func (m *Manager) removeRule(r Rule) error {
	dnatPre := []string{
		"-t", "nat", "-D", "PREROUTING", "-p", r.Protocol, "--dport", fmt.Sprint(r.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort),
	}
	_ = run("iptables", dnatPre) // -D on a missing rule just errors; ignore
	dnatOut := []string{
		"-t", "nat", "-D", "OUTPUT", "-p", r.Protocol, "--dport", fmt.Sprint(r.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", r.ContainerIP, r.ContainerPort),
	}
	_ = run("iptables", dnatOut)
	mask := []string{
		"-t", "nat", "-D", "POSTROUTING", "-s", r.ContainerIP + "/32", "-o", m.wan, "-j", "MASQUERADE",
	}
	_ = run("iptables", mask)
	_ = run("iptables", []string{"-D", "FORWARD", "-d", r.ContainerIP + "/32", "-j", "ACCEPT"})
	_ = run("iptables", []string{"-D", "FORWARD", "-s", r.ContainerIP + "/32", "-j", "ACCEPT"})
	return nil
}

// ensureRule checks for an existing rule with -C and adds it if absent,
// making the agent's operations idempotent across restarts. It normalizes both
// append (-A) and insert (-I chain pos) forms for the check, since -C matches
// a rule regardless of its position in the chain.
func ensureRule(bin string, args []string) error {
	check := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-A":
			check = append(check, "-C")
		case "-I":
			check = append(check, "-C")
			// -I <chain> <pos> ... → -C <chain> ... : keep the chain, drop
			// the insert position.
			if i+2 < len(args) {
				check = append(check, args[i+1])
				i += 2
			}
		default:
			check = append(check, args[i])
		}
	}
	if err := run(bin, check); err == nil {
		return nil // already present
	}
	return run(bin, args)
}

func run(bin string, args []string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w (%s)", bin, args, err, stderr.String())
	}
	return nil
}
