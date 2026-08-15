package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// incusProvider talks to the Incus (or LXD) REST API over its unix socket.
// Endpoints follow the documented /1.0 API; async operations are polled via
// /1.0/operations/<id>/wait.
type incusProvider struct {
	hc      *http.Client
	pool    string
	network string

	// ipv6Mode/ipv6Subnet mirror the agent config: subnet mode uses the public
	// container subnet with bridge NAT off (addresses routed, NDP handled by
	// the agent); snat mode uses a ULA subnet with bridge NAT on.
	ipv6Mode   string
	ipv6Subnet string

	cpuMu   sync.Mutex
	cpuPrev map[string]cpuSample
}

type cpuSample struct {
	at    time.Time
	usage uint64 // cumulative nanoseconds
}

func newIncus(socketPath string, opts Options) (*incusProvider, error) {
	if socketPath == "" {
		socketPath = "/var/lib/incus/unix.socket"
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
	return &incusProvider{
		hc:         &http.Client{Transport: tr, Timeout: 90 * time.Second},
		pool:       firstNonEmpty(opts.Pool, "default"),
		network:    firstNonEmpty(opts.Network, "lxdbr0"),
		ipv6Mode:   mode,
		ipv6Subnet: opts.IPv6Subnet,
		cpuPrev:    make(map[string]cpuSample),
	}, nil
}

const incusBase = "http://unix"

func (p *incusProvider) request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, incusBase+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("incus: decode %s: %w", path, err)
	}
	if code := toInt(out["status_code"]); code != 200 && code != 0 {
		return nil, fmt.Errorf("incus %s %s: %s", method, path, toString(out["error"]))
	}
	return out, nil
}

// wait polls an async operation until it completes or times out.
func (p *incusProvider) wait(ctx context.Context, opURL string) error {
	out, err := p.request(ctx, http.MethodGet, opURL+"/wait?timeout=30", nil)
	if err != nil {
		return err
	}
	if code := toInt(out["status_code"]); code != 200 {
		return fmt.Errorf("incus operation failed: %s", toString(out["err"]))
	}
	return nil
}

// ensureNet6 creates the IPv6-enabled bridge network once, if missing. The
// bridge is dual-stack: IPv4 stays NAT'ed (10.93.0.0/20, distinct from the
// OCI bridge's 10.91.0.0/20 to avoid collisions on mixed hosts), while IPv6
// uses the configured container subnet with bridge NAT off in subnet mode
// (addresses are routed; the agent's NDP responder advertises them) or on in
// snat mode (ULA + dnsmasq NAT). Stateful DHCPv6 hands out explicit addresses
// so the agent can NDP-proxy each container's public IP deterministically.
func (p *incusProvider) ensureNet6(ctx context.Context) error {
	// Already present?
	out, err := p.request(ctx, http.MethodGet, "/1.0/networks", nil)
	if err != nil {
		return err
	}
	if items, ok := out["metadata"].([]any); ok {
		for _, it := range items {
			if strings.HasSuffix(toString(it), "/"+incusNet6) {
				// Note: the existing bridge is NOT re-configured here. If a node
				// switches from snat to subnet mode (config edit), the stale ULA
				// subnet / ipv6.nat stays until the network is recreated or the
				// node reinstalled — same caveat as the OCI pre-existing network.
				return nil
			}
		}
	}

	subnet := p.ipv6Subnet
	if subnet == "" {
		subnet = "fd00:10:91::/64"
	}
	gw := v6Gateway(subnet)
	if gw == "" {
		return fmt.Errorf("invalid ipv6 subnet %q", subnet)
	}
	nat := "true"
	if p.ipv6Mode == "subnet" {
		nat = "false" // public addresses must not be masqueraded
	}
	body := map[string]any{
		"name":        incusNet6,
		"description": "codetest IPv6 bridge",
		"type":        "bridge",
		"config": map[string]string{
			"ipv4.address":       "10.93.0.1/20",
			"ipv4.nat":           "true",
			"ipv6.address":       gw,
			"ipv6.nat":           nat,
			"ipv6.dhcp":          "true",
			"ipv6.dhcp.stateful": "true",
		},
	}
	out, err = p.request(ctx, http.MethodPost, "/1.0/networks", body)
	if err != nil {
		// Two concurrent instance creates can both pass the exists check above
		// (TOCTOU); the loser must NOT fail the create — treat "already exists"
		// as success so the second instance still attaches to the bridge.
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return nil
		}
		return err
	}
	if op := toString(out["operation"]); op != "" {
		return p.wait(ctx, op)
	}
	return nil
}

// v6Gateway returns the gateway address for an IPv6 CIDR (network + 1) in
// "addr/prefix" form, or "" for a non-IPv6/invalid subnet.
func v6Gateway(subnet string) string {
	_, n, err := net.ParseCIDR(strings.TrimSpace(subnet))
	if err != nil {
		return ""
	}
	gw := n.IP.To16()
	if gw == nil || n.IP.To4() != nil {
		return "" // IPv4 or invalid
	}
	for i := len(gw) - 1; i >= 0; i-- {
		gw[i]++
		if gw[i] != 0 {
			break
		}
	}
	ones, _ := n.Mask.Size()
	return gw.String() + "/" + strconv.Itoa(ones)
}

// incusNet6 is the IPv6-enabled bridge used by instances with IPv6 enabled
// (dual-stack: IPv4 NAT on, IPv6 public/ULA per mode). Same name as the OCI
// bridge so the NDP responder config (ndp_network) is backend-agnostic.
const incusNet6 = "narwhal-net6"

func (p *incusProvider) Create(ctx context.Context, spec Spec) (Info, error) {
	instanceCfg := map[string]string{
		"limits.cpu":    strconv.Itoa(spec.CpuCores),
		"limits.memory": fmt.Sprintf("%dMiB", spec.MemoryMB),
	}
	// LXD/Incus swap is backed by host swap; enabling it mirrors the
	// platform's "swap = memory" default.
	if spec.SwapMB > 0 {
		instanceCfg["limits.memory.swap"] = "true"
	}
	// IPv6-enabled instances join the dual-stack bridge so they get a public
	// (subnet) or ULA (snat) v6 address. If the bridge cannot be prepared the
	// instance is still created — it just runs without IPv6 on the default
	// network, mirroring the OCI provider's graceful degradation.
	eth0Network := p.network
	if spec.IPv6 && p.ipv6Mode != "" {
		if err := p.ensureNet6(ctx); err == nil {
			eth0Network = incusNet6
		}
	}
	body := map[string]any{
		"name":   spec.Name,
		"source": map[string]string{"type": "image", "alias": spec.Image},
		"config": instanceCfg,
		"devices": map[string]any{
			"root": map[string]string{
				"type": "disk", "pool": p.pool, "path": "/",
				"size": fmt.Sprintf("%dGiB", spec.DiskMB/1024),
			},
			"eth0": map[string]string{"type": "nic", "network": eth0Network, "name": "eth0"},
		},
	}
	out, err := p.request(ctx, http.MethodPost, "/1.0/instances", body)
	if err != nil {
		return Info{}, err
	}
	if op := toString(out["operation"]); op != "" {
		if err := p.wait(ctx, op); err != nil {
			return Info{}, err
		}
	}
	if err := p.stateAction(ctx, spec.Name, "start", false); err != nil {
		return Info{}, err
	}
	return p.Get(ctx, spec.Name)
}

func (p *incusProvider) Start(ctx context.Context, name string) error {
	return p.stateAction(ctx, name, "start", false)
}

func (p *incusProvider) Stop(ctx context.Context, name string, force bool) error {
	return p.stateAction(ctx, name, "stop", force)
}

func (p *incusProvider) Restart(ctx context.Context, name string) error {
	return p.stateAction(ctx, name, "restart", false)
}

func (p *incusProvider) stateAction(ctx context.Context, name, action string, force bool) error {
	body := map[string]any{"action": action}
	if force {
		body["force"] = true
	}
	out, err := p.request(ctx, http.MethodPut, "/1.0/instances/"+name+"/state", body)
	if err != nil {
		return err
	}
	if op := toString(out["operation"]); op != "" {
		return p.wait(ctx, op)
	}
	return nil
}

func (p *incusProvider) Delete(ctx context.Context, name string) error {
	out, err := p.request(ctx, http.MethodDelete, "/1.0/instances/"+name, nil)
	if err != nil {
		return err
	}
	if op := toString(out["operation"]); op != "" {
		return p.wait(ctx, op)
	}
	return nil
}

// Rebuild replaces the instance's rootfs image while keeping its name and
// network identity. LXD/Incus support rebuilding by PUT with a new source.
func (p *incusProvider) Rebuild(ctx context.Context, name string, spec Spec) error {
	if err := p.stateAction(ctx, name, "stop", true); err != nil {
		return err
	}
	// Fetch the current config/devices/profiles so the PUT doesn't wipe them.
	out, err := p.request(ctx, http.MethodGet, "/1.0/instances/"+name, nil)
	if err != nil {
		return err
	}
	md := asMap(out["metadata"])
	body := map[string]any{
		"name":     name,
		"source":   map[string]string{"type": "image", "alias": spec.Image},
		"config":   md["config"],
		"devices":  md["devices"],
		"profiles": md["profiles"],
	}
	if _, err := p.request(ctx, http.MethodPut, "/1.0/instances/"+name, body); err != nil {
		return err
	}
	return p.stateAction(ctx, name, "start", false)
}

func (p *incusProvider) Get(ctx context.Context, name string) (Info, error) {
	out, err := p.request(ctx, http.MethodGet, "/1.0/instances/"+name, nil)
	if err != nil {
		return Info{}, err
	}
	md := asMap(out["metadata"])
	info := Info{
		Name:   toString(md["name"]),
		Status: strings.ToLower(toString(md["status"])),
	}
	info.IP, info.IPv6, _ = p.instanceIPs(ctx, name)
	return info, nil
}

func (p *incusProvider) List(ctx context.Context) ([]Info, error) {
	out, err := p.request(ctx, http.MethodGet, "/1.0/instances", nil)
	if err != nil {
		return nil, err
	}
	items, _ := out["metadata"].([]any)
	res := make([]Info, 0, len(items))
	for _, it := range items {
		m := asMap(it)
		res = append(res, Info{
			Name:   toString(m["name"]),
			Status: strings.ToLower(toString(m["status"])),
		})
	}
	return res, nil
}

func (p *incusProvider) Stats(ctx context.Context, name string) (Stats, error) {
	out, err := p.request(ctx, http.MethodGet, "/1.0/instances/"+name+"/state", nil)
	if err != nil {
		return Stats{}, err
	}
	md := asMap(out["metadata"])
	st := Stats{Status: strings.ToLower(toString(md["status"]))}

	cpu := asMap(md["cpu"])
	cpuUsage := toUint64(cpu["usage"])
	st.CPUPercent = p.cpuPercent(name, cpuUsage)

	mem := asMap(md["memory"])
	st.MemoryUsedMB = int64(toUint64(mem["usage"]) / (1024 * 1024))
	st.MemoryLimitMB = int64(toUint64(mem["usage_limit"]) / (1024 * 1024))

	if netm, ok := md["network"].(map[string]any); ok {
		for _, v := range netm {
			iface := asMap(v)
			counters := asMap(iface["counters"])
			st.RxBytes += toUint64(counters["bytes_received"])
			st.TxBytes += toUint64(counters["bytes_sent"])
		}
	}
	return st, nil
}

// instanceIPs returns the first IPv4 and first global IPv6 address across all
// instance interfaces (for subnet mode the agent NDP-proxies the IPv6 one).
func (p *incusProvider) instanceIPs(ctx context.Context, name string) (ipv4, ipv6 string, err error) {
	out, err := p.request(ctx, http.MethodGet, "/1.0/instances/"+name+"/state", nil)
	if err != nil {
		return "", "", err
	}
	md := asMap(out["metadata"])
	if netm, ok := md["network"].(map[string]any); ok {
		for _, v := range netm {
			iface := asMap(v)
			addrs, _ := iface["addresses"].([]any)
			for _, a := range addrs {
				am := asMap(a)
				switch toString(am["family"]) {
				case "inet":
					if ipv4 == "" {
						ipv4 = toString(am["address"])
					}
				case "inet6":
					// Skip link-local and the engine's gateway-side addresses;
					// keep the first global unicast address.
					if ip := toString(am["address"]); ip != "" && ipv6 == "" {
						if parsed := net.ParseIP(strings.Split(ip, "%")[0]); parsed != nil && parsed.IsGlobalUnicast() {
							ipv6 = ip
						}
					}
				}
			}
		}
	}
	return ipv4, ipv6, nil
}

// cpuPercent computes CPU usage % from the cumulative usage counter delta.
func (p *incusProvider) cpuPercent(name string, usage uint64) float64 {
	p.cpuMu.Lock()
	defer p.cpuMu.Unlock()
	prev, ok := p.cpuPrev[name]
	now := time.Now()
	p.cpuPrev[name] = cpuSample{at: now, usage: usage}
	if !ok || usage < prev.usage {
		return 0
	}
	dt := now.Sub(prev.at).Seconds()
	if dt <= 0 {
		return 0
	}
	return (float64(usage-prev.usage) / 1e9) / dt * 100
}

// ---- small helpers for the loosely typed API responses ----

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

func toUint64(v any) uint64 {
	switch n := v.(type) {
	case float64:
		return uint64(n)
	case int:
		return uint64(n)
	case int64:
		return uint64(n)
	case uint64:
		return n
	case string:
		if i, err := strconv.ParseUint(n, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
