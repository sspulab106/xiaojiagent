package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/agent/internal/firewall"
)

// newFirewallClient builds the rfw proxy client from config. Returns nil when
// rfw is disabled (empty address), in which case the handlers report it.
func newFirewallClient(addr string) *firewall.Client {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return firewall.New(addr)
}

// FirewallInfo returns a compact firewall snapshot for health/status payloads.
// The probe result is cached for a few seconds so a down rfw daemon never adds
// up to ~1.2s of latency to every health poll (panel 5s, master 30s).
func (s *Service) FirewallInfo() map[string]any {
	if s.fw == nil {
		return map[string]any{"installed": false, "api_addr": s.cfg.RFWAddr, "rule_count": 0, "iface": ""}
	}
	s.fwMu.Lock()
	defer s.fwMu.Unlock()
	if s.fwInfo != nil && time.Since(s.fwInfoT) < 5*time.Second {
		return s.fwInfo
	}
	out := map[string]any{
		"installed":  false,
		"api_addr":   s.cfg.RFWAddr,
		"rule_count": 0,
		"iface":      "",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	if st, err := s.fw.Status(ctx); err == nil {
		out["installed"] = true
		out["rule_count"] = st.RuleCount
		out["iface"] = st.Iface
	}
	s.fwInfo = out
	s.fwInfoT = time.Now()
	return out
}

// firewallRuleReq is the master-facing rule payload. It mirrors rfw's wire
// format (flattened ip_config) so the master can pass it through verbatim.
type firewallRuleReq struct {
	Priority  *uint32  `json:"priority"`
	Enabled   *bool    `json:"enabled"`
	Direction string   `json:"direction"`
	Protocol  string   `json:"protocol"`
	PortStart *uint16  `json:"port_start"`
	PortEnd   *uint16  `json:"port_end"`
	IPType    string   `json:"ip_type"`
	IP        string   `json:"ip"`
	Countries []string `json:"countries"`
	Action    string   `json:"action"`
}

// validateRule normalizes/validates a rule payload before sending it to rfw.
func validateRule(req *firewallRuleReq) (firewall.CreateRule, string) {
	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction != "in" && direction != "out" {
		return firewall.CreateRule{}, "direction 仅支持 in / out"
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	switch protocol {
	case "tcp", "udp", "http", "tls", "socks", "ssh", "fet", "wireguard", "openvpn", "quic", "all":
	default:
		return firewall.CreateRule{}, "protocol 不受支持: " + req.Protocol
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "block" && action != "pass" {
		return firewall.CreateRule{}, "action 仅支持 block / pass"
	}
	ipType := strings.ToLower(strings.TrimSpace(req.IPType))
	if ipType == "" {
		ipType = "any"
	}
	switch ipType {
	case "any":
	case "cidr":
		if strings.TrimSpace(req.IP) == "" {
			return firewall.CreateRule{}, "cidr 模式需要 ip 字段（如 1.2.3.0/24）"
		}
	case "geoip":
		if len(req.Countries) == 0 {
			return firewall.CreateRule{}, "geoip 模式需要 countries 列表（如 CN）"
		}
		// rfw 的 GeoIP 查询需要大写两位国家码；统一归一化避免小写导致抓取失败
		countries := make([]string, 0, len(req.Countries))
		for _, cc := range req.Countries {
			if c := strings.ToUpper(strings.TrimSpace(cc)); c != "" {
				countries = append(countries, c)
			}
		}
		req.Countries = countries
	default:
		return firewall.CreateRule{}, "ip_type 仅支持 any / cidr / geoip"
	}

	portStart := uint16(0)
	if req.PortStart != nil {
		portStart = *req.PortStart
	}
	priority := uint32(0)
	if req.Priority != nil {
		priority = *req.Priority
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return firewall.CreateRule{
		Priority:  priority,
		Enabled:   enabled,
		Direction: direction,
		Protocol:  protocol,
		PortStart: portStart,
		PortEnd:   req.PortEnd,
		IPType:    ipType,
		IP:        strings.TrimSpace(req.IP),
		Countries: req.Countries,
		Action:    action,
	}, ""
}

// HandleFWStatus reports whether rfw is installed and its rule count.
func (s *Service) HandleFWStatus(c *gin.Context) {
	if s.fw == nil {
		fail(c, http.StatusBadRequest, "rfw 防火墙未配置（RFW_ADDR 为空）")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	st, err := s.fw.Status(ctx)
	if err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	ok(c, st)
}

// HandleFWList returns all firewall rules.
func (s *Service) HandleFWList(c *gin.Context) {
	if s.fw == nil {
		fail(c, http.StatusBadRequest, "rfw 防火墙未配置（RFW_ADDR 为空）")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	rules, err := s.fw.ListRules(ctx)
	if err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	ok(c, rules)
}

// HandleFWCreate validates and creates a firewall rule.
func (s *Service) HandleFWCreate(c *gin.Context) {
	if s.fw == nil {
		fail(c, http.StatusBadRequest, "rfw 防火墙未配置（RFW_ADDR 为空）")
		return
	}
	var req firewallRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体无效: "+err.Error())
		return
	}
	rule, msg := validateRule(&req)
	if msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	created, err := s.fw.CreateRule(ctx, rule)
	if err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	ok(c, created)
}

// HandleFWDelete removes a rule by its rfw numeric id.
func (s *Service) HandleFWDelete(c *gin.Context) {
	if s.fw == nil {
		fail(c, http.StatusBadRequest, "rfw 防火墙未配置（RFW_ADDR 为空）")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "规则 ID 无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := s.fw.DeleteRule(ctx, id); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	ok(c, nil)
}
