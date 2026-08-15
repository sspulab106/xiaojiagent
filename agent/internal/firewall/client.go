// Package firewall is a thin HTTP client for the rfw eBPF firewall daemon
// (https://github.com/narwhal-cloud/rfw). rfw listens on a local address
// (127.0.0.1:7734 by default) and exposes a small JSON API; the agent proxies
// it to the master under /agent/v1/firewall so the web panel can manage rules
// without exposing rfw to the network.
//
// Rule JSON mirrors rfw's wire format: a flattened IpConfig is serialized as
// ip_type ("any" | "cidr" | "geoip") plus ip / countries.
package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Rule is one firewall rule as returned by rfw (RuleResponse).
type Rule struct {
	ID        uint64   `json:"id"`
	Priority  uint32   `json:"priority"`
	Enabled   bool     `json:"enabled"`
	Direction string   `json:"direction"` // in | out
	Protocol  string   `json:"protocol"`  // tcp|udp|http|tls|socks|ssh|fet|wireguard|openvpn|quic|all
	PortStart uint16   `json:"port_start"`
	PortEnd   uint16   `json:"port_end"`
	IPType    string   `json:"ip_type"`             // any | cidr | geoip
	IP        string   `json:"ip,omitempty"`        // cidr: e.g. 1.2.3.0/24
	Countries []string `json:"countries,omitempty"` // geoip: e.g. ["CN"]
	Action    string   `json:"action"`              // block | pass
}

// Status is rfw's /api/status response.
type Status struct {
	Iface      string `json:"iface"`
	APIVersion string `json:"api_version"`
	RuleCount  int    `json:"rule_count"`
}

// CreateRule is the POST /api/rules payload (CreateRuleRequest).
type CreateRule struct {
	Priority  uint32   `json:"priority"`
	Enabled   bool     `json:"enabled"`
	Direction string   `json:"direction"`
	Protocol  string   `json:"protocol"`
	PortStart uint16   `json:"port_start"`
	PortEnd   *uint16  `json:"port_end,omitempty"`
	IPType    string   `json:"ip_type"`
	IP        string   `json:"ip,omitempty"`
	Countries []string `json:"countries,omitempty"`
	Action    string   `json:"action"`
}

// Client talks to one rfw daemon.
type Client struct {
	base string
	hc   *http.Client
}

func New(addr string) *Client {
	base := strings.TrimSuffix(addr, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{base: base, hc: &http.Client{Timeout: 5 * time.Second}}
}

// do performs a request and decodes the JSON response.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("rfw 不可用（%v），请确认 rfw 已安装并运行", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("rfw %s %s: %s", method, path, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Status returns rfw's interface/rule-count status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.do(ctx, http.MethodGet, "/api/status", nil, &s)
	return s, err
}

// ListRules returns all rules, sorted by rfw (priority desc).
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var rules []Rule
	err := c.do(ctx, http.MethodGet, "/api/rules", nil, &rules)
	return rules, err
}

// CreateRule adds a rule and returns it with its assigned ID.
func (c *Client) CreateRule(ctx context.Context, req CreateRule) (Rule, error) {
	var rule Rule
	err := c.do(ctx, http.MethodPost, "/api/rules", req, &rule)
	return rule, err
}

// DeleteRule removes a rule by numeric id.
func (c *Client) DeleteRule(ctx context.Context, id uint64) error {
	return c.do(ctx, http.MethodDelete, "/api/rules/"+strconv.FormatUint(id, 10), nil, nil)
}
