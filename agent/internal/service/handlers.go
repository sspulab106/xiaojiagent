package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/agent/internal/terminal"
)

// HandleHealth reports host status and totals; the master polls this every
// 30 seconds to keep node records fresh. Mirrors the NarwhalCloud AgentGateway
// heartbeat fields (cpu/ram/disk/load/net-rate/uptime/bandwidth/ipv6/vms).
func (s *Service) HandleHealth(c *gin.Context) {
	ok(c, s.HealthData(c.Request.Context()))
}

// Health returns a live host snapshot for the local management panel.
func (s *Service) Health() map[string]any {
	return s.HealthData(context.Background())
}

// HealthData gathers the live host + VM snapshot used by both the master's
// health poll and the local management panel.
func (s *Service) HealthData(ctx context.Context) map[string]any {
	cpu, memUsed, memTotal, diskUsed, diskTotal, cores := hostStats()
	load1, load5, load15 := readLoadavg()
	uptime := readUptime()
	netIn, netOut := s.netSampler.rates(s.cfg.WanIface)
	vms, _ := s.prov.List(ctx)
	running := 0
	for _, v := range vms {
		if v.Status == "running" {
			running++
		}
	}
	return map[string]any{
		"status":             "ok",
		"host_ip":            detectPublicIP(),
		"host_ipv6":          detectPublicIPv6(),
		"ipv6_mode":          s.cfg.IPv6Mode,
		"ipv6_addr":          s.cfg.IPv6Addr,
		"ipv6_subnet":        s.cfg.IPv6Subnet,
		"ndp_iface":          s.cfg.NdpIface,
		"ndp_subnets":        s.cfg.NdpSubnets,
		"ndp":                s.ndp.Info(),
		"firewall":           s.FirewallInfo(),
		"host_cpu_percent":   cpu,
		"host_mem_used_mb":   memUsed,
		"host_mem_total_mb":  memTotal,
		"host_disk_used_mb":  diskUsed,
		"host_disk_total_mb": diskTotal,
		"total_cores":        cores,
		"load1":              load1,
		"load5":              load5,
		"load15":             load15,
		"uptime":             uptime,
		"net_in_bps":         netIn,
		"net_out_bps":        netOut,
		"bandwidth_mbps":     s.cfg.BandwidthMbps,
		"virt_type":          s.cfg.VirtType,
		"os_images":          osImages,
		"total_vms":          len(vms),
		"running_vms":        running,
	}
}

func (s *Service) HandleList(c *gin.Context) {
	items, err := s.prov.List(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, items)
}

func (s *Service) HandleGet(c *gin.Context) {
	info, err := s.prov.Get(c.Request.Context(), c.Param("name"))
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, info)
}

func (s *Service) HandleCreate(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体无效: "+err.Error())
		return
	}
	if req.Name == "" || req.Image == "" {
		fail(c, http.StatusBadRequest, "name 与 image 必填")
		return
	}
	req.Name = Normalize(req.Name)
	resp, err := s.CreateInstance(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, resp)
}

type actionReq struct {
	Action   string `json:"action"`
	Image    string `json:"image"`    // rebuild 时可选：重装为指定镜像
	Password string `json:"password"` // rebuild 时可选：重装后设置 root 密码并预装 sshd
}

func (s *Service) HandleAction(c *gin.Context) {
	var req actionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	info, err := s.Action(c.Request.Context(), c.Param("name"), req.Action, req.Image, req.Password)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, info)
}

func (s *Service) HandleDelete(c *gin.Context) {
	if err := s.DeleteInstance(c.Request.Context(), c.Param("name")); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, nil)
}

func (s *Service) HandleStats(c *gin.Context) {
	st, err := s.prov.Stats(c.Request.Context(), c.Param("name"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, st)
}

func (s *Service) HandleAddPort(c *gin.Context) {
	var req AddPortRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体无效: "+err.Error())
		return
	}
	rule, err := s.AddPort(c.Request.Context(), c.Param("name"), req)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, rule)
}

func (s *Service) HandleRemovePort(c *gin.Context) {
	if err := s.RemovePort(c.Param("id")); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, nil)
}

type changePasswordReq struct {
	Password string `json:"password"`
}

// HandleChangePassword updates the root password inside a container.
func (s *Service) HandleChangePassword(c *gin.Context) {
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	if err := s.ChangePassword(c.Request.Context(), c.Param("name"), req.Password); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, nil)
}

// HandleTerminal opens a pty shell inside the container.
func (s *Service) HandleTerminal(c *gin.Context) {
	cmd := s.TerminalCommand(c.Param("name"))
	terminal.Handle(c, cmd)
}

// HandleSelfCheckStart launches verify-ndp.sh asynchronously and returns the
// run id; the master polls HandleSelfCheckStatus for the output.
func (s *Service) HandleSelfCheckStart(c *gin.Context) {
	id, err := s.selfcheck.Start()
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"id": id, "status": "running"})
}

// HandleSelfCheckStatus returns the current snapshot of a self-check run.
func (s *Service) HandleSelfCheckStatus(c *gin.Context) {
	run, found := s.selfcheck.Status(c.Param("id"))
	if !found {
		fail(c, http.StatusNotFound, "自检任务不存在或已过期")
		return
	}
	ok(c, run)
}

// ---- response helpers ----

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message, "data": nil})
}

// ---- host stats ----

var (
	statMu      sync.Mutex
	lastCPUStat cpuStat
	lastCPUAt   time.Time
)

type cpuStat struct{ user, nice, system, idle, iowait, irq, softirq, steal uint64 }

func hostStats() (cpuPercent float64, memUsedMB, memTotalMB, diskUsedMB, diskTotalMB int64, cores int) {
	cpuPercent = readCPUPercent()
	memTotalMB, memUsedMB = readMeminfo()
	diskUsedMB, diskTotalMB = readDisk()
	cores = runtime.NumCPU()
	return
}

func readCPUPercent() float64 {
	var st cpuStat
	if !readCPUTimes(&st) {
		return 0
	}
	statMu.Lock()
	defer statMu.Unlock()
	now := time.Now()
	prev := lastCPUStat
	prevAt := lastCPUAt
	lastCPUStat = st
	lastCPUAt = now
	if prevAt.IsZero() {
		return 0
	}
	idle := (st.idle - prev.idle) + (st.iowait - prev.iowait)
	total := (st.user + st.nice + st.system + st.idle + st.iowait + st.irq + st.softirq + st.steal) -
		(prev.user + prev.nice + prev.system + prev.idle + prev.iowait + prev.irq + prev.softirq + prev.steal)
	if total == 0 {
		return 0
	}
	return (1 - float64(idle)/float64(total)) * 100
}

func readCPUTimes(st *cpuStat) bool {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return false
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "cpu ") {
			line = l
			break
		}
	}
	if line == "" {
		return false
	}
	var u, n, sy, id, io, ir, sir, sv uint64
	if _, err := fmt.Sscanf(line, "cpu %d %d %d %d %d %d %d %d", &u, &n, &sy, &id, &io, &ir, &sir, &sv); err != nil {
		return false
	}
	*st = cpuStat{u, n, sy, id, io, ir, sir, sv}
	return true
}

func readMeminfo() (totalMB, usedMB int64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB, availableKB int64
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			fmt.Sscanf(strings.TrimPrefix(l, "MemTotal:"), "%d", &totalKB)
		} else if strings.HasPrefix(l, "MemAvailable:") {
			fmt.Sscanf(strings.TrimPrefix(l, "MemAvailable:"), "%d", &availableKB)
		}
	}
	return totalKB / 1024, (totalKB - availableKB) / 1024
}

func readDisk() (usedMB, totalMB int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, 0
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	free := int64(st.Bfree) * bsize
	return (total - free) / 1024 / 1024, total / 1024 / 1024
}

// detectPublicIP finds the outbound IPv4 without sending packets.
func detectPublicIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// detectPublicIPv6 finds the first global unicast IPv6 address on the host.
func detectPublicIPv6() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.To4() == nil && ip.IsGlobalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}

// readLoadavg parses the 1/5/15-minute load averages from /proc/loadavg.
func readLoadavg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	_, _ = fmt.Sscanf(string(b), "%f %f %f", &l1, &l5, &l15)
	return l1, l5, l15
}

// readUptime returns host uptime in seconds from /proc/uptime.
func readUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var sec float64
	_, _ = fmt.Sscanf(string(b), "%f", &sec)
	return int64(sec)
}

// netSampler computes per-interface bytes/sec rates from /proc/net/dev
// counters, using deltas between calls.
type netSampler struct {
	mu   sync.Mutex
	prev map[string]devCounters
	at   time.Time
}

type devCounters struct {
	rx, tx uint64
}

func newNetSampler() *netSampler {
	return &netSampler{prev: make(map[string]devCounters)}
}

// rates returns (rxBps, txBps) for the given interface.
func (n *netSampler) rates(iface string) (int64, int64) {
	if iface == "" {
		return 0, 0
	}
	now := time.Now()
	cur := readDevCounters(iface)
	n.mu.Lock()
	defer n.mu.Unlock()
	prev, ok := n.prev[iface]
	n.prev[iface] = cur
	if !ok || n.at.IsZero() {
		n.at = now
		return 0, 0
	}
	dt := now.Sub(n.at).Seconds()
	n.at = now
	if dt <= 0 {
		return 0, 0
	}
	// Counter resets (interface down/up) produce negative deltas; ignore.
	in := int64(0)
	out := int64(0)
	if cur.rx >= prev.rx {
		in = int64(float64(cur.rx-prev.rx) / dt)
	}
	if cur.tx >= prev.tx {
		out = int64(float64(cur.tx-prev.tx) / dt)
	}
	return in, out
}

// readDevCounters reads rx/tx byte counters for one interface from
// /proc/net/dev (rx and tx bytes are fields 1 and 9 of the line).
func readDevCounters(iface string) devCounters {
	var c devCounters
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return c
	}
	for _, line := range strings.Split(string(b), "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 || strings.TrimSpace(line[:idx]) != iface {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		_, _ = fmt.Sscanf(fields[0], "%d", &c.rx)
		_, _ = fmt.Sscanf(fields[8], "%d", &c.tx)
		return c
	}
	return c
}

// osImages is the list of OS presets the agent advertises, kept in sync with
// the master's /api/v1/images.
var osImages = []string{"debian:13-slim", "alpine:latest", "ubuntu:24.04"}
