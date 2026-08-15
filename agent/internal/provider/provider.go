// Package provider defines the virtualization backend abstraction and ships
// two implementations:
//
//   - incusProvider: the Incus/LXD REST API over a unix socket (system
//     containers and VMs). This is the recommended backend for NAT VPS.
//   - ociProvider: the Docker-compatible engine API over a unix socket, which
//     also works with Podman's compat socket.
//
// Everything else in the agent (NAT, monitoring, terminal) is backend-agnostic
// and talks to this interface.
package provider

import "context"

// Spec describes an instance to create.
type Spec struct {
	Name     string
	Image    string
	CpuCores int
	MemoryMB int64
	DiskMB   int64
	SwapMB   int64
	IPv6     bool
	// Cmd optionally overrides the container's default command (OCI only).
	Cmd []string
}

// Info is a snapshot of an instance.
type Info struct {
	Name   string `json:"name"`
	Status string `json:"status"` // running | stopped | ...
	IP     string `json:"ip"`
	// IPv6 is the instance's global IPv6 address, if any (subnet mode: a
	// public address the agent NDP-proxies; snat mode: a ULA address).
	IPv6 string `json:"ipv6"`
}

// Stats are live resource counters for one instance.
type Stats struct {
	Status        string  `json:"status"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsedMB  int64   `json:"memory_used_mb"`
	MemoryLimitMB int64   `json:"memory_limit_mb"`
	RxBytes       uint64  `json:"rx_bytes"` // cumulative bytes received since start
	TxBytes       uint64  `json:"tx_bytes"` // cumulative bytes sent since start
}

// Provider is the virtualization backend contract.
type Provider interface {
	Create(ctx context.Context, spec Spec) (Info, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string, force bool) error
	Restart(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error
	Rebuild(ctx context.Context, name string, spec Spec) error
	Get(ctx context.Context, name string) (Info, error)
	List(ctx context.Context) ([]Info, error)
	Stats(ctx context.Context, name string) (Stats, error)
}

// Options carries backend-specific settings.
type Options struct {
	Pool    string // incus storage pool
	Network string // incus bridge network

	// IPv6 settings mirror the agent config: mode is "none"/"snat"/"subnet";
	// Subnet is the container v6 subnet (public /112 in subnet mode, ULA /64 in
	// snat mode) used to create the IPv6-enabled bridge network.
	IPv6Mode   string
	IPv6Subnet string
}

// New builds the provider selected by virtType ("incus" or "oci").
func New(virtType, socketPath string, opts Options) (Provider, error) {
	switch virtType {
	case "oci":
		return newOCI(socketPath, opts)
	default:
		return newIncus(socketPath, opts)
	}
}
