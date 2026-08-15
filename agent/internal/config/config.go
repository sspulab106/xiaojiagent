// Package config loads agent settings. Values can come from environment
// variables or from a config.json in the agent data directory. Environment
// variables win over defaults; the JSON file wins over everything. The web
// panel writes token/web_password back to the JSON file via Save.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	// Listen is the agent HTTP(S) listen address.
	Listen string `json:"listen"`
	// Token is the shared bearer token the master must present.
	Token string `json:"token"`
	// WebPassword guards the local management panel (http://host:8792).
	WebPassword string `json:"web_password"`
	// VirtType selects the backend: "incus" (LXD/Incus) or "oci" (Docker/Podman).
	VirtType string `json:"virt_type"`
	// SocketPath is the unix socket for the virtualization backend API.
	SocketPath string `json:"socket_path"`
	// CLIBin is the CLI used for exec/terminal (incus | lxc | docker | podman).
	CLIBin string `json:"cli_bin"`
	// DataDir stores agent state (state.json) and optional config.json.
	DataDir string `json:"data_dir"`
	// WanIface is the public-facing interface used in MASQUERADE rules.
	WanIface string `json:"wan_iface"`
	// BandwidthMbps is the host's total available bandwidth (for monitoring).
	BandwidthMbps int `json:"bandwidth_mbps"`
	// MasterURL is the master's public base URL, used by the panel's
	// self-update (downloads the newest agent binary).
	MasterURL string `json:"master_url"`
	// PortStart/PortEnd define the allocatable host port range.
	PortStart int `json:"port_start"`
	PortEnd   int `json:"port_end"`
	// IncusPool/IncusNetwork configure the storage pool and bridge network.
	IncusPool    string `json:"incus_pool"`
	IncusNetwork string `json:"incus_network"`

	// RFWAddr is the local rfw eBPF firewall API (127.0.0.1:7734). Empty
	// disables the agent's firewall endpoints.
	RFWAddr string `json:"rfw_addr"`

	// IPv6 mode mirrors the install script's detection: "none" (default),
	// "snat" (private ULA + host MASQUERADE) or "subnet" (public subnet
	// per-container addresses + built-in NDP responder).
	IPv6Mode   string `json:"ipv6_mode"`
	IPv6Addr   string `json:"ipv6_addr"`   // host's public IPv6 (subnet mode)
	IPv6Subnet string `json:"ipv6_subnet"` // container v6 subnet: public /112 (subnet) or ULA /64 (snat)
	IPv6Iface  string `json:"ipv6_iface"`

	// NDP responder settings (subnet mode only). NdpIface is the WAN interface
	// the agent proxies neighbor solicitations out of; NdpSubnets is a
	// comma-separated list of container subnets whose addresses get proxy
	// entries; NdpNetwork is the bridge network name (informational).
	NdpIface   string `json:"ndp_iface"`
	NdpSubnets string `json:"ndp_subnets"`
	NdpNetwork string `json:"ndp_network"`

	// VerifyNdpScript is the path to scripts/verify-ndp.sh, the NDP
	// responder end-to-end check that the remote self-check endpoint runs.
	// Defaults to <DataDir>/verify-ndp.sh (the install script ships it there).
	VerifyNdpScript string `json:"verify_ndp_script"`

	// path is the config.json location; never serialized.
	path string `json:"-"`
}

// Path returns the on-disk config.json path.
func (c *Config) Path() string { return c.path }

// Save persists the current configuration to config.json.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o600)
}

// SetToken updates the token in memory and persists it to config.json.
func (c *Config) SetToken(token string) error {
	c.Token = token
	return c.Save()
}

// SetWebPassword updates the panel password in memory and persists it.
func (c *Config) SetWebPassword(pw string) error {
	c.WebPassword = pw
	return c.Save()
}

func Load() Config {
	cfg := Config{
		Listen:          env("AGENT_LISTEN", ":8792"),
		Token:           env("AGENT_TOKEN", "change-me"),
		WebPassword:     env("AGENT_WEB_PASSWORD", ""),
		VirtType:        env("VIRT_TYPE", "incus"),
		DataDir:         env("AGENT_DATA_DIR", "/opt/codetest-agent"),
		WanIface:        env("WAN_IFACE", "eth0"),
		BandwidthMbps:   envInt("BANDWIDTH_MBPS", 0),
		MasterURL:       env("AGENT_MASTER_URL", ""),
		PortStart:       envInt("PORT_START", 20000),
		PortEnd:         envInt("PORT_END", 40000),
		IncusPool:       env("INCUS_POOL", "default"),
		IncusNetwork:    env("INCUS_NETWORK", "lxdbr0"),
		RFWAddr:         env("RFW_ADDR", "127.0.0.1:7734"),
		IPv6Mode:        env("IPV6_MODE", ""),
		IPv6Addr:        env("IPV6_ADDR", ""),
		IPv6Subnet:      env("IPV6_SUBNET", "fd00:10:91::/64"),
		IPv6Iface:       env("IPV6_IFACE", ""),
		NdpIface:        env("NDP_IFACE", ""),
		NdpSubnets:      env("NDP_SUBNETS", ""),
		NdpNetwork:      env("NDP_NETWORK", ""),
		VerifyNdpScript: env("VERIFY_NDP_SCRIPT", ""),
	}
	cfg.path = filepath.Join(cfg.DataDir, "config.json")
	if cfg.VerifyNdpScript == "" {
		cfg.VerifyNdpScript = filepath.Join(cfg.DataDir, "verify-ndp.sh")
	}

	// Normalize virt type and pick sensible defaults for socket/CLI.
	switch cfg.VirtType {
	case "oci", "docker", "podman":
		cfg.VirtType = "oci"
		if cfg.CLIBin == "" {
			cfg.CLIBin = env("OCI_CLI", "docker")
		}
		if cfg.SocketPath == "" {
			cfg.SocketPath = env("OCI_SOCKET", "/var/run/docker.sock")
		}
	default:
		cfg.VirtType = "incus"
		if cfg.CLIBin == "" {
			cfg.CLIBin = "incus"
		}
		if cfg.SocketPath == "" {
			cfg.SocketPath = "/var/lib/incus/unix.socket"
		}
	}

	// Optional config.json overrides (only if present).
	if data, err := os.ReadFile(cfg.path); err == nil {
		_ = json.Unmarshal(data, &cfg)
		cfg.path = filepath.Join(cfg.DataDir, "config.json")
	}

	return cfg
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
