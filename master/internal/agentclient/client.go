// Package agentclient is the HTTP client the master uses to talk to agent
// daemons running on host nodes. Agents authenticate via a shared bearer
// token configured per node. Responses use the same envelope as the master.
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client talks to one agent instance.
type Client struct {
	BaseURL string
	Token   string
	hc      *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Token:   token,
		// Generous timeout: instance creation includes image pull and in-container
		// SSH provisioning (apt/apk install) which can take a while.
		hc: &http.Client{Timeout: 300 * time.Second},
	}
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Request/response types mirror the agent API.

type CreateInstanceReq struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	CpuCores int    `json:"cpu_cores"`
	MemoryMB int64  `json:"memory_mb"`
	DiskMB   int64  `json:"disk_mb"`
	SwapMB   int64  `json:"swap_mb"`
	IPv6     bool   `json:"ipv6"`
}

type CreateInstanceResp struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	IP           string `json:"ip"`
	SSHPort      int    `json:"ssh_port"`
	SSHRuleID    string `json:"ssh_rule_id"`
	RootPassword string `json:"root_password"`
}

type InstanceInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	IP     string `json:"ip"`
	IPv6   string `json:"ipv6"`
}

type StatsResp struct {
	Status        string  `json:"status"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsedMB  int64   `json:"memory_used_mb"`
	MemoryLimitMB int64   `json:"memory_limit_mb"`
	RxBytes       uint64  `json:"rx_bytes"`
	TxBytes       uint64  `json:"tx_bytes"`
}

type PortResp struct {
	ID            string `json:"id"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
	ContainerIP   string `json:"container_ip"`
}

// FirewallRule mirrors the agent's (rfw) rule format.
type FirewallRule struct {
	ID        uint64   `json:"id"`
	Priority  uint32   `json:"priority"`
	Enabled   bool     `json:"enabled"`
	Direction string   `json:"direction"`
	Protocol  string   `json:"protocol"`
	PortStart uint16   `json:"port_start"`
	PortEnd   uint16   `json:"port_end"`
	IPType    string   `json:"ip_type"`
	IP        string   `json:"ip,omitempty"`
	Countries []string `json:"countries,omitempty"`
	Action    string   `json:"action"`
}

// FirewallStatus is rfw's /api/status snapshot.
type FirewallStatus struct {
	Iface      string `json:"iface"`
	APIVersion string `json:"api_version"`
	RuleCount  int    `json:"rule_count"`
}

// CreateFirewallRuleReq is the POST body forwarded to the agent.
type CreateFirewallRuleReq struct {
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

type HealthResp struct {
	Status          string  `json:"status"`
	HostIP          string  `json:"host_ip"`
	HostIPv6        string  `json:"host_ipv6"`
	IPv6Mode        string  `json:"ipv6_mode"`
	IPv6Subnet      string  `json:"ipv6_subnet"`
	NdpIface        string  `json:"ndp_iface"`
	NdpSubnets      string  `json:"ndp_subnets"`
	HostCPUPercent  float64 `json:"host_cpu_percent"`
	HostMemUsedMB   int64   `json:"host_mem_used_mb"`
	HostMemTotalMB  int64   `json:"host_mem_total_mb"`
	HostDiskUsedMB  int64   `json:"host_disk_used_mb"`
	HostDiskTotalMB int64   `json:"host_disk_total_mb"`
	TotalCores      int     `json:"total_cores"`
	Load1           float64 `json:"load1"`
	Load5           float64 `json:"load5"`
	Load15          float64 `json:"load15"`
	Uptime          int64   `json:"uptime"`
	NetInBps        int64   `json:"net_in_bps"`
	NetOutBps       int64   `json:"net_out_bps"`
	BandwidthMbps   int     `json:"bandwidth_mbps"`
	VirtType        string  `json:"virt_type"`
	TotalVMs        int     `json:"total_vms"`
	RunningVMs      int     `json:"running_vms"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("agent returned invalid response: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("agent error %d: %s", env.Code, env.Message)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (c *Client) Health(ctx context.Context) (*HealthResp, error) {
	var out HealthResp
	err := c.do(ctx, http.MethodGet, "/agent/v1/health", nil, &out)
	return &out, err
}

func (c *Client) ListInstances(ctx context.Context) ([]InstanceInfo, error) {
	var out []InstanceInfo
	err := c.do(ctx, http.MethodGet, "/agent/v1/instances", nil, &out)
	return out, err
}

func (c *Client) CreateInstance(ctx context.Context, req CreateInstanceReq) (*CreateInstanceResp, error) {
	var out CreateInstanceResp
	err := c.do(ctx, http.MethodPost, "/agent/v1/instances", req, &out)
	return &out, err
}

func (c *Client) InstanceAction(ctx context.Context, name, action, image, password string) (*InstanceInfo, error) {
	var out InstanceInfo
	body := map[string]string{"action": action}
	if image != "" {
		body["image"] = image
	}
	if password != "" {
		body["password"] = password
	}
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/instances/%s/action", name), body, &out)
	return &out, err
}

func (c *Client) DeleteInstance(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agent/v1/instances/%s", name), nil, nil)
}

func (c *Client) Stats(ctx context.Context, name string) (*StatsResp, error) {
	var out StatsResp
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/agent/v1/instances/%s/stats", name), nil, &out)
	return &out, err
}

func (c *Client) AddPort(ctx context.Context, name string, containerPort int, protocol string, hostPort int) (*PortResp, error) {
	var out PortResp
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/instances/%s/ports", name),
		map[string]any{"container_port": containerPort, "protocol": protocol, "host_port": hostPort}, &out)
	return &out, err
}

func (c *Client) RemovePort(ctx context.Context, ruleID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agent/v1/ports/%s", ruleID), nil, nil)
}

// ChangePassword asks the agent to update the container's root password.
func (c *Client) ChangePassword(ctx context.Context, name, password string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/instances/%s/password", name),
		map[string]string{"password": password}, nil)
}

// SelfCheckStart is the agent's reply to POST /agent/v1/selfcheck.
type SelfCheckStart struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// SelfCheckStatus mirrors the agent's self-check run snapshot.
type SelfCheckStatus struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"` // running | done | failed
	Output     string    `json:"output"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// StartSelfCheck asks the agent to run scripts/verify-ndp.sh in the
// background and returns the run id for polling.
func (c *Client) StartSelfCheck(ctx context.Context) (*SelfCheckStart, error) {
	var out SelfCheckStart
	err := c.do(ctx, http.MethodPost, "/agent/v1/selfcheck", nil, &out)
	return &out, err
}

// SelfCheckStatus polls a self-check run's live output/status.
func (c *Client) SelfCheckStatus(ctx context.Context, runID string) (*SelfCheckStatus, error) {
	var out SelfCheckStatus
	err := c.do(ctx, http.MethodGet, "/agent/v1/selfcheck/"+runID, nil, &out)
	return &out, err
}

// FirewallStatus returns the node's rfw status snapshot.
func (c *Client) FirewallStatus(ctx context.Context) (*FirewallStatus, error) {
	var out FirewallStatus
	err := c.do(ctx, http.MethodGet, "/agent/v1/firewall/status", nil, &out)
	return &out, err
}

// FirewallRules lists the node's firewall rules.
func (c *Client) FirewallRules(ctx context.Context) ([]FirewallRule, error) {
	var out []FirewallRule
	err := c.do(ctx, http.MethodGet, "/agent/v1/firewall/rules", nil, &out)
	return out, err
}

// FirewallCreateRule adds a rule on the node and returns it with its ID.
func (c *Client) FirewallCreateRule(ctx context.Context, req CreateFirewallRuleReq) (*FirewallRule, error) {
	var out FirewallRule
	err := c.do(ctx, http.MethodPost, "/agent/v1/firewall/rules", req, &out)
	return &out, err
}

// FirewallDeleteRule removes a rule on the node by rfw numeric id.
func (c *Client) FirewallDeleteRule(ctx context.Context, ruleID uint64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/agent/v1/firewall/rules/%d", ruleID), nil, nil)
}
