export interface User {
  id: number
  username: string
  role: 'admin' | 'user'
  created_at: string
  instance_quota: number
  expires_at: string | null
  banned: boolean
  email?: string
  email_verified?: boolean
  balance_cents: number
  hosting_total_cents: number
  hosting_available_cents: number
  hosting_frozen_cents: number
}

// 技术支持工单：用户针对自己的实例提工单，路由到宿主机技术支持页面。
export interface Ticket {
  id: number
  user_id: number
  node_id: number
  instance_id: number
  title: string
  content: string
  status: 'open' | 'resolved'
  resolved_at: string | null
  created_at: string
  updated_at: string
  node_read: boolean
  user_read: boolean
  user_name?: string
  node_name?: string
  instance_name?: string
  replies?: TicketReply[]
  reply_count?: number
}

export interface TicketReply {
  id: number
  ticket_id: number
  user_id: number
  content: string
  created_at: string
  user_name?: string
}

export interface TicketUnread {
  node_unread: number
  user_unread: number
}

export interface PlatformSettings {
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass: string
  smtp_from: string
  email_verify_enabled: string
  cloudflare_captcha_enabled: string
}

export interface Transaction {
  id: number
  user_id: number
  type: 'recharge' | 'purchase' | 'refund' | 'host_income' | 'host_refund' | 'sale_income' | 'admin_adjust'
  amount_cents: number
  status: 'success' | 'pending'
  description: string
  created_at: string
}

export interface Coupon {
  id: number
  node_id: number
  user_id: number
  code: string
  percent_off: number
  max_uses: number
  used_count: number
  enabled: boolean
  expires_at: string | null
  created_at: string
}

export interface UserDetail {
  user: User
  nodes: Node[]
  instances: Instance[]
}

export interface Package {
  id: number
  node_id: number
  user_id: number
  name: string
  images: string // comma-separated image refs
  cpu_cores: number
  memory_mb: number
  disk_mb: number
  traffic_gb: number
  port_slots: number
  ipv6: boolean
  price_cents: number
  listed: boolean
  enabled: boolean
  early_full_refund: boolean // 允许早期全额退款
  created_at: string
  node_name?: string
  node_region?: string
}

// 交易市场挂单（二手实例买卖，按剩余时间/流量折合剩余价值定价）
export interface MarketListing {
  id: number
  instance_id: number
  seller_id: number
  buyer_id: number
  price_cents: number
  status: 'listed' | 'sold' | 'cancelled'
  created_at: string
  sold_at: string | null
  cancelled_at: string | null
  seller_name?: string
  value_cents?: number
  time_value_cents?: number
  traffic_ratio?: number
  remaining_days?: number
  expired?: boolean
  instance?: Instance
}

export interface GiftCard {
  id: number
  code: string
  amount_cents: number
  status: 'issued' | 'redeemed'
  created_by: number
  redeemed_by: number
  redeemed_at: string | null
  expires_at: string | null
  created_at: string
}

export interface Node {
  id: number
  name: string
  agent_addr: string
  status: 'online' | 'offline'
  region: string
  host_ip: string
  total_memory_mb: number
  total_cores: number
  total_disk_mb: number
  country?: string
  tags?: string
  notes?: string
  ipv6?: string
  ipv6_mode?: string
  ipv6_subnet?: string
  ndp_iface?: string
  ndp_subnets?: string
  last_selfcheck?: string | null // JSON: {status, run_id, exit_code, started_at, finished_at, output}
  bandwidth_mbps?: number
  mem_used_mb?: number
  disk_used_mb?: number
  load1?: number
  load5?: number
  load15?: number
  uptime?: number
  net_in_bps?: number
  net_out_bps?: number
  instance_count?: number
}

export interface Instance {
  id: number
  name: string
  display_name: string
  image: string
  status: string
  ip: string
  cpu_cores: number
  memory_mb: number
  disk_mb: number
  traffic_gb: number
  port_slots: number
  expires_at: string | null
  traffic_used_up_bytes: number
  traffic_used_down_bytes: number
  node_name: string
  node_region: string
  node_host_ip: string
  ssh_password: string
  package_name: string
  created_at: string
  auto_renew: boolean
  price_cents: number
  paid_cents?: number
  country: string
  os: string
  early_full_refund?: boolean
}

// 节点自检（远程运行 verify-ndp.sh）的运行快照
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export interface SelfCheckStatus {
  id: string
  status: 'running' | 'done' | 'failed'
  output: string
  exit_code: number
  started_at: string
  finished_at: string
}

export interface NodeStats {
  host: {
    cpu_percent: number
    mem_used_mb: number
    mem_total_mb: number
    disk_used_mb: number
    disk_total_mb: number
    load1: number
    load5: number
    load15: number
    net_in_bps: number
    net_out_bps: number
    uptime: number
    total_cores: number
    running_vms: number
    total_vms: number
    ipv6_mode?: string
    ipv6_subnet?: string
    ndp_iface?: string
    ndp_subnets?: string
  }
  vms: Array<{
    name: string
    status: string
    cpu_percent: number
    mem_used_mb: number
    mem_limit_mb: number
  }>
}

export interface PortMapping {
  id: number
  host_port: number
  container_ip: string
  container_port: number
  protocol: string
}

export interface InstanceStats {
  status: string
  cpu_percent: number
  memory_used_mb: number
  memory_limit_mb: number
  rx_bytes: number
  tx_bytes: number
  traffic_used_up_bytes: number
  traffic_used_down_bytes: number
}

export interface AdminStats {
  nodes: number
  nodes_online: number
  instances: number
  users: number
}

export interface ImagePreset {
  id: string
  name: string
  ref: string
}

export interface HostResult {
  node: Node
  token: string
  script: string
}

export interface Announcement {
  id: number
  title: string
  content: string
  created_at: string
}

export interface NodeDetail {
  node: Node & { token: string }
  stats?: Record<string, unknown>
}

export interface FirewallRule {
  id: number
  priority: number
  enabled: boolean
  direction: 'in' | 'out'
  protocol: string
  port_start: number
  port_end: number
  ip_type: 'any' | 'cidr' | 'geoip'
  ip?: string
  countries?: string[]
  action: 'block' | 'pass'
}

export interface FirewallStatus {
  iface: string
  api_version: string
  rule_count: number
}

export interface NodeFirewall {
  status: FirewallStatus
  rules: FirewallRule[]
}
