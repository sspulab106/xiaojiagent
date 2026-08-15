package model

import "time"

// User is a tenant (or admin) of the platform. Quotas are stored directly on
// the user; subscription expiry gates instance creation. Wallet fields support
// the control-panel balance cards (amounts in cents).
type User struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Username      string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash  string     `json:"-"`
	Role          string     `gorm:"size:16;default:user" json:"role"` // admin | user
	InstanceQuota int        `gorm:"default:2" json:"instance_quota"`
	ExpiresAt     *time.Time `json:"expires_at"`                  // subscription end; nil = no limit
	Banned        bool       `gorm:"default:false" json:"banned"` // 封禁：禁止登录与所有 API
	CreatedAt     time.Time  `json:"created_at"`

	// 个人信息：邮箱（绑定后用于接收工单/通知；是否必须验证由平台设置决定）。
	Email             string     `gorm:"size:128" json:"email"`
	EmailVerified     bool       `gorm:"default:false" json:"email_verified"`
	EmailVerifyCode   string     `gorm:"size:16" json:"-"` // 邮箱验证码（明文存储，仅用于演示）
	EmailVerifyExpiry *time.Time `json:"-"`

	// Wallet.
	BalanceCents          int64 `gorm:"default:0" json:"balance_cents"`           // 可用余额
	HostingTotalCents     int64 `gorm:"default:0" json:"hosting_total_cents"`     // 托管余额(总)
	HostingAvailableCents int64 `gorm:"default:0" json:"hosting_available_cents"` // 托管余额(可用)
	HostingFrozenCents    int64 `gorm:"default:0" json:"hosting_frozen_cents"`    // 托管余额(冻结)
}

// Package is a purchasable plan (套餐). Instances snapshot these limits at
// creation time, so later package edits don't affect running instances.
// A package is created by a host owner (UserID) for their node (NodeID) and
// must be Listed to be buyable by other users; NodeID == 0 means a
// platform-level package managed by an admin.
type Package struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	NodeID     uint      `gorm:"index;default:0" json:"node_id"` // 0 = platform package
	UserID     uint      `gorm:"default:0" json:"user_id"`       // owning host owner
	Name       string    `gorm:"size:64" json:"name"`
	Images     string    `gorm:"size:512" json:"images"` // comma-separated image refs the buyer may pick
	CpuCores   int       `json:"cpu_cores"`
	MemoryMB   int64     `json:"memory_mb"`
	DiskMB     int64     `json:"disk_mb"`
	TrafficGB  int64     `json:"traffic_gb"` // monthly allowance
	PortSlots  int       `json:"port_slots"` // max NAT port mappings
	IPv6       bool      `json:"ipv6"`
	PriceCents int       `json:"price_cents"`
	Listed     bool      `gorm:"default:false" json:"listed"` // 上架：买家可见可购
	Enabled    bool      `gorm:"default:true" json:"enabled"` // soft-disable
	// EarlyFullRefund 允许早期全额退款：取消机器时，开通 ≤1 小时且流量 ≤1GB 的
	// 实例全额退款；未开启的套餐取消一律不退款。
	EarlyFullRefund bool      `gorm:"default:false" json:"early_full_refund"`
	CreatedAt       time.Time `json:"created_at"`

	// Computed, populated by handlers (not DB columns).
	NodeName   string `gorm:"-" json:"node_name"`
	NodeRegion string `gorm:"-" json:"node_region"`
}

// Node is a registered host (母鸡) running the agent daemon. It belongs to a
// user in the hosting center; platform-level nodes may have UserID == 0.
type Node struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	UserID    uint   `json:"user_id"` // owning user (hosting center)
	Name      string `gorm:"size:64;uniqueIndex" json:"name"`
	AgentAddr string `gorm:"size:128" json:"agent_addr"` // e.g. http://10.0.0.1:8792
	Token     string `gorm:"size:256" json:"-"`
	Status    string `gorm:"size:16;default:offline" json:"status"` // online | offline
	Region    string `gorm:"size:64" json:"region"`
	Country   string `gorm:"size:64" json:"country"`
	Tags      string `gorm:"size:128" json:"tags"`     // comma-separated: 独立,US,PODMAN
	Notes     string `gorm:"size:256" json:"notes"`    // user remark
	VirtType  string `gorm:"size:16" json:"virt_type"` // incus | oci
	HostIP    string `gorm:"size:64" json:"host_ip"`
	IPv6      string `gorm:"size:64" json:"ipv6"`

	// IPv6 mode snapshot from the agent's health poll: "none" | "snat" |
	// "subnet". Subnet mode additionally carries the container subnet and the
	// NDP responder settings so admins can see each node's IPv6 topology.
	IPv6Mode   string `gorm:"size:16" json:"ipv6_mode"`
	IPv6Subnet string `gorm:"size:64" json:"ipv6_subnet"`
	NdpIface   string `gorm:"size:32" json:"ndp_iface"`
	NdpSubnets string `gorm:"size:128" json:"ndp_subnets"`

	// LastSelfcheck is a JSON snapshot of the most recent remote self-check
	// (verify-ndp.sh) run: {status, run_id, exit_code, started_at,
	// finished_at, output}. Empty until the first self-check.
	LastSelfcheck string `gorm:"type:text" json:"last_selfcheck"`

	CreatedAt time.Time `json:"created_at"`

	// Latest health snapshot, refreshed by the node health loop.
	TotalCores    int     `json:"total_cores"`
	TotalMemoryMB int64   `json:"total_memory_mb"`
	TotalDiskMB   int64   `json:"total_disk_mb"`
	MemUsedMB     int64   `json:"mem_used_mb"`
	DiskUsedMB    int64   `json:"disk_used_mb"`
	BandwidthMbps int     `json:"bandwidth_mbps"`
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
	Uptime        int64   `json:"uptime"`
	NetInBps      int64   `json:"net_in_bps"`
	NetOutBps     int64   `json:"net_out_bps"`

	// Computed (not a DB column): number of instances on this node.
	InstanceCount int64 `gorm:"-" json:"instance_count"`
}

// Instance is a tenant's container/VM. Resource fields are snapshotted from
// the package at creation. Traffic accounting uses a monthly window with
// counter baselines (LastRxBytes/LastTxBytes) so agent restarts are safe.
type Instance struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:64" json:"name"` // safe ASCII container name (podman/docker safe charset)
	UserID    uint   `json:"user_id"`
	NodeID    uint   `json:"node_id"`
	PackageID uint   `json:"package_id"`
	Image     string `gorm:"size:128" json:"image"`
	Status    string `gorm:"size:16;default:stopped" json:"status"`
	IP        string `gorm:"size:64" json:"ip"`

	// DisplayName is the user-visible label (may contain CJK/unicode); the
	// container itself uses Name.
	DisplayName string `gorm:"size:128" json:"display_name"`

	CpuCores    int        `json:"cpu_cores"`
	MemoryMB    int64      `json:"memory_mb"`
	DiskMB      int64      `json:"disk_mb"`
	SwapMB      int64      `json:"swap_mb"`
	SSHPassword string     `json:"ssh_password"`
	TrafficGB   int64      `json:"traffic_gb"` // 0 = 无限流量
	PortSlots   int        `json:"port_slots"`
	PriceCents  int64      `json:"price_cents"` // 套餐原价（快照）
	PaidCents   int64      `json:"paid_cents"`  // 买家实际支付（原价 - 折扣）
	AutoRenew   bool       `gorm:"default:true" json:"auto_renew"`
	ExpiresAt   *time.Time `json:"expires_at"`

	TrafficMonth    string `gorm:"size:8" json:"traffic_month"` // YYYY-MM
	TrafficUsedUp   int64  `json:"traffic_used_up_bytes"`       // current month, bytes in
	TrafficUsedDown int64  `json:"traffic_used_down_bytes"`     // current month, bytes out
	LastRxBytes     int64  `json:"-"`
	LastTxBytes     int64  `json:"-"`

	CreatedAt time.Time `json:"created_at"`

	// Computed, populated by handlers (not DB columns).
	NodeName        string `gorm:"-" json:"node_name"`
	NodeRegion      string `gorm:"-" json:"node_region"`
	NodeHostIP      string `gorm:"-" json:"node_host_ip"`
	PackageName     string `gorm:"-" json:"package_name"`
	Country         string `gorm:"-" json:"country"`
	OS              string `gorm:"-" json:"os"`
	EarlyFullRefund bool   `gorm:"-" json:"early_full_refund"` // 所属套餐是否允许早期全额退款
}

// Announcement is a platform notice shown on the control panel.
type Announcement struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:128" json:"title"`
	Content   string    `gorm:"size:1024" json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// PortMapping is one NAT rule: host_port:container_ip:container_port.
// AgentRuleID is the agent-side rule identifier used for deletion.
type PortMapping struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	InstanceID    uint      `json:"instance_id"`
	AgentRuleID   string    `gorm:"size:64" json:"-"`
	HostPort      int       `json:"host_port"`
	ContainerIP   string    `gorm:"size:64" json:"container_ip"`
	ContainerPort int       `json:"container_port"`
	Protocol      string    `gorm:"size:8;default:tcp" json:"protocol"` // tcp | udp
	CreatedAt     time.Time `json:"created_at"`
}

// TrafficLog is an optional daily traffic history. Not yet written by any
// job; kept as the schema for future billing/usage reports.
type TrafficLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	InstanceID uint   `json:"instance_id"`
	Date       string `gorm:"size:16;index" json:"date"` // YYYY-MM-DD
	BytesUp    int64  `json:"bytes_up"`
	BytesDown  int64  `json:"bytes_down"`
}

// Transaction is one billing record shown on the control panel's 交易记录
// page. Type is recharge | purchase | refund | host_income | host_refund.
// AmountCents is always positive.
type Transaction struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"index" json:"user_id"`
	Type        string    `json:"type"`
	AmountCents int64     `json:"amount_cents"` // positive amounts
	Status      string    `json:"status"`       // success | pending
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Coupon is a purchase discount code (优惠码): percent_off off the package
// price when creating an instance. NodeID == 0 means platform-wide; otherwise
// the coupon only applies to packages on that node. Owned by a host owner
// (UserID) or the platform (0).
type Coupon struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	NodeID     uint       `gorm:"index;default:0" json:"node_id"` // 0 = platform-wide
	UserID     uint       `gorm:"default:0" json:"user_id"`
	Code       string     `gorm:"uniqueIndex;size:32" json:"code"`
	PercentOff int        `json:"percent_off"` // 0-100, discount % on purchase
	MaxUses    int        `json:"max_uses"`    // 0 = unlimited
	UsedCount  int        `json:"used_count"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"` // nil = 永不过期
	CreatedAt  time.Time  `json:"created_at"`
}

// MarketListing is a second-hand listing on the trading marketplace (交易市场):
// a seller puts an unexpired instance up for sale at their chosen price, and
// any other user can buy it. The system computes the reference remaining value
// (剩余价值) from the remaining time and remaining monthly traffic; the seller
// sets the actual price (bounded by what they originally paid). On purchase the
// instance ownership transfers and its PaidCents becomes the sale price so
// later refunds stay proportional to what the current owner actually paid.
type MarketListing struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	InstanceID  uint       `gorm:"index" json:"instance_id"`
	SellerID    uint       `gorm:"index" json:"seller_id"`
	BuyerID     uint       `json:"buyer_id"`
	PriceCents  int64      `json:"price_cents"` // seller-set asking price
	Status      string     `gorm:"size:16;default:listed" json:"status"` // listed | sold | cancelled
	CreatedAt   time.Time  `json:"created_at"`
	SoldAt      *time.Time `json:"sold_at"`
	CancelledAt *time.Time `json:"cancelled_at"`

	// Computed, populated by handlers (not DB columns).
	SellerName     string           `gorm:"-" json:"seller_name"`
	ValueCents     int64            `gorm:"-" json:"value_cents"`      // 折合剩余价值
	TimeValueCents int64            `gorm:"-" json:"time_value_cents"` // 剩余时间价值分量
	TrafficRatio   int64            `gorm:"-" json:"traffic_ratio"`    // 流量剩余率 0-100
	RemainingDays  float64          `gorm:"-" json:"remaining_days"`
	Expired        bool       `gorm:"-" json:"expired"`
	Instance       *Instance  `gorm:"-" json:"instance"`
}

// Ticket is a support ticket (工单): a user reports a problem with one of
// their instances, and the ticket is routed to the host node owner's support
// queue (平台管理员也能看到全部工单). The owner can reply and mark it
// resolved; the user sees the thread and unread state.
type Ticket struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"index" json:"user_id"` // 发起人（用户）
	NodeID     uint       `gorm:"index" json:"node_id"` // 目标宿主机（工单路由到该节点的技术支持）
	InstanceID uint       `json:"instance_id"`
	Title      string     `gorm:"size:128" json:"title"`
	Content    string     `gorm:"type:text" json:"content"` // 首条消息内容
	Status     string     `gorm:"size:16;default:open" json:"status"` // open | resolved
	ResolvedAt *time.Time `json:"resolved_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// 未读标记：NodeRead 为 false 表示宿主机/管理员还没看；UserRead 为 false
	// 表示用户还没看到最新回复（用于技术支持和列表页的未读气泡）。
	NodeRead bool `gorm:"default:false" json:"node_read"`
	UserRead bool `gorm:"default:true" json:"user_read"`

	// Computed, populated by handlers.
	UserName     string    `gorm:"-" json:"user_name"`
	NodeName     string    `gorm:"-" json:"node_name"`
	InstanceName string    `gorm:"-" json:"instance_name"`
	Replies      []TicketReply `gorm:"-" json:"replies,omitempty"`
	ReplyCount   int       `gorm:"-" json:"reply_count"`
}

// TicketReply is one message in a support ticket thread.
type TicketReply struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TicketID  uint      `gorm:"index" json:"ticket_id"`
	UserID    uint      `json:"user_id"`
	Content   string    `gorm:"type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`

	// Computed.
	UserName string `gorm:"-" json:"user_name"`
}

// Setting is a platform-level key/value setting (发件邮箱、邮箱验证开关等)，
// edited by admins in the management panel.
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// GiftCard is an admin-issued redemption code (兑换码/礼品卡) that credits the
// redeeming user's balance once, like a recharge.
type GiftCard struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Code        string     `gorm:"uniqueIndex;size:32" json:"code"`
	AmountCents int64      `json:"amount_cents"`
	Status      string     `gorm:"size:16;default:issued" json:"status"` // issued | redeemed
	CreatedBy   uint       `json:"created_by"`
	RedeemedBy  uint       `json:"redeemed_by"`
	RedeemedAt  *time.Time `json:"redeemed_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}
