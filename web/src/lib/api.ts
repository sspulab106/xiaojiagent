import type { AdminStats, Announcement, Coupon, FirewallRule, GiftCard, HostResult, ImagePreset, Instance, InstanceStats, MarketListing, Node, NodeDetail, NodeFirewall, NodeStats, Package, PlatformSettings, PortMapping, SelfCheckStatus, Ticket, TicketReply, TicketUnread, Transaction, User, UserDetail } from './types'

const BASE = '/api/v1'

let token: string | null = localStorage.getItem('token')

export function setToken(t: string | null) {
  token = t
  if (t) localStorage.setItem('token', t)
  else localStorage.removeItem('token')
}

export function getToken() {
  return token
}

export class ApiError extends Error {
  constructor(
    message: string,
    public code: number,
    public status: number,
  ) {
    super(message)
  }
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(BASE + path, { ...opts, headers })
  const body = await res.json().catch(() => ({ code: -1, message: '无效响应', data: null }))
  if (body.code !== 0) throw new ApiError(body.message || `HTTP ${res.status}`, body.code, res.status)
  return body.data as T
}

export const api = {
  login: (username: string, password: string) =>
    request<{ token: string; user: User }>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  register: (username: string, password: string) =>
    request<{ token: string; user: User }>('/auth/register', { method: 'POST', body: JSON.stringify({ username, password }) }),
  profile: () => request<User>('/user/profile'),
  packages: () => request<Package[]>('/packages'),
  announcements: () => request<Announcement[]>('/announcements'),
  recharge: (amount_cents: number) =>
    request<User>('/user/recharge', {
      method: 'POST',
      body: JSON.stringify({ amount_cents }),
    }),
  redeemGiftCard: (code: string) =>
    request<User>('/user/redeem', { method: 'POST', body: JSON.stringify({ code }) }),
  transactions: () => request<Transaction[]>('/user/transactions'),
  coupons: () => request<Coupon[]>('/user/coupons'),
  adminCoupons: () => request<Coupon[]>('/admin/coupons'),
  addCoupon: (body: { code: string; percent_off: number; max_uses: number; enabled: boolean; expires_at?: string | null }) =>
    request<Coupon>('/admin/coupons', { method: 'POST', body: JSON.stringify(body) }),
  updateCoupon: (id: number, body: { percent_off?: number; max_uses?: number; enabled?: boolean; expires_at?: string | null; clear_expiry?: boolean }) =>
    request<Coupon>(`/admin/coupons/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteCoupon: (id: number) => request<null>(`/admin/coupons/${id}`, { method: 'DELETE' }),
  instances: () => request<Instance[]>('/instances'),
  setAutoRenew: (id: number, enabled: boolean) =>
    request<Instance>(`/instances/${id}/auto-renew`, { method: 'POST', body: JSON.stringify({ enabled }) }),
  nodes: () => request<Node[]>('/nodes'),
  addMachine: (body: { name: string; agent_addr?: string; region?: string; virt_type: string; with_token: boolean; web_password?: string }) =>
    request<HostResult>('/nodes', { method: 'POST', body: JSON.stringify(body) }),
  nodeDetail: (id: number) => request<NodeDetail>(`/nodes/${id}`),
  nodeStats: (id: number) => request<NodeStats>(`/nodes/${id}/stats`),
  createInstance: (body: { package_id: number; image: string; name?: string; coupon_code?: string }) =>
    request<Instance>('/instances', { method: 'POST', body: JSON.stringify(body) }),
  images: () => request<ImagePreset[]>('/images'),
  addHost: (body: { name: string; agent_addr?: string; region?: string; virt_type: string; with_token: boolean; web_password?: string }) =>
    request<HostResult>('/admin/hosts', { method: 'POST', body: JSON.stringify(body) }),
  instance: (id: number) => request<Instance>(`/instances/${id}`),
  changePassword: (id: number, password: string) =>
    request<Instance>(`/instances/${id}/password`, { method: 'POST', body: JSON.stringify({ password }) }),
  instanceAction: (id: number, action: string, image?: string) =>
    request<Instance>(`/instances/${id}/action`, {
      method: 'POST',
      body: JSON.stringify(image ? { action, image } : { action }),
    }),
  deleteInstance: (id: number) => request<null>(`/instances/${id}`, { method: 'DELETE' }),
  stats: (id: number) => request<InstanceStats>(`/instances/${id}/stats`),
  ports: (id: number) => request<PortMapping[]>(`/instances/${id}/ports`),
  addPort: (id: number, body: { container_port: number; protocol: string; host_port?: number }) =>
    request<PortMapping>(`/instances/${id}/ports`, { method: 'POST', body: JSON.stringify(body) }),
  deletePort: (mappingId: number) => request<null>(`/ports/${mappingId}`, { method: 'DELETE' }),
  adminStats: () => request<AdminStats>('/admin/stats'),
  adminNodes: () => request<Node[]>('/admin/nodes'),
  addNode: (body: { name: string; agent_addr?: string; token: string; region?: string }) =>
    request<Node>('/admin/nodes', { method: 'POST', body: JSON.stringify(body) }),
  deleteNode: (id: number) => request<null>(`/admin/nodes/${id}`, { method: 'DELETE' }),
  nodeInstallScript: (id: number, body: { with_token: boolean; web_password?: string; virt_type: string }) =>
    request<{ script: string }>(`/admin/nodes/${id}/install-script`, { method: 'POST', body: JSON.stringify(body) }),
  // 节点自检（远程运行 verify-ndp.sh，异步 + 轮询）
  // 管理后台（admin）
  adminNodeSelfCheck: (id: number) =>
    request<{ run_id: string; status: string }>(`/admin/nodes/${id}/selfcheck`, { method: 'POST' }),
  adminNodeSelfCheckStatus: (id: number, runId: string) =>
    request<SelfCheckStatus>(`/admin/nodes/${id}/selfcheck/${runId}`),
  // 托管中心（机主）
  nodeSelfCheck: (id: number) =>
    request<{ run_id: string; status: string }>(`/nodes/${id}/selfcheck`, { method: 'POST' }),
  nodeSelfCheckStatus: (id: number, runId: string) =>
    request<SelfCheckStatus>(`/nodes/${id}/selfcheck/${runId}`),
  adminUsers: () => request<User[]>('/admin/users'),
  extendUser: (id: number, days: number) =>
    request<User>(`/admin/users/${id}/extend`, { method: 'POST', body: JSON.stringify({ days }) }),
  adminUserDetail: (id: number) => request<UserDetail>(`/admin/users/${id}`),
  banUser: (id: number, banned: boolean) =>
    request<User>(`/admin/users/${id}/ban`, { method: 'POST', body: JSON.stringify({ banned }) }),
  resetUserPassword: (id: number, password: string) =>
    request<null>(`/admin/users/${id}/password`, { method: 'POST', body: JSON.stringify({ password }) }),
  adjustUserBalance: (id: number, delta_cents: number, remark?: string) =>
    request<User>(`/admin/users/${id}/balance`, { method: 'POST', body: JSON.stringify({ delta_cents, remark }) }),
  // 平台套餐（admin）
  addPackage: (body: Partial<Package>) =>
    request<Package>('/admin/packages', { method: 'POST', body: JSON.stringify(body) }),
  // 节点套餐（机主/admin）
  nodePackages: (nodeId: number) => request<Package[]>(`/nodes/${nodeId}/packages`),
  addNodePackage: (nodeId: number, body: {
    name: string; images?: string[]; cpu_cores?: number; memory_mb?: number; disk_mb?: number;
    traffic_gb?: number; port_slots?: number; ipv6?: boolean; price_cents?: number; listed?: boolean;
    early_full_refund?: boolean;
  }) => request<Package>(`/nodes/${nodeId}/packages`, { method: 'POST', body: JSON.stringify(body) }),
  updatePackage: (id: number, body: Partial<Package>) =>
    request<Package>(`/packages/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deletePackage: (id: number) => request<null>(`/packages/${id}`, { method: 'DELETE' }),
  // 节点优惠码（机主/admin）
  nodeCoupons: (nodeId: number) => request<Coupon[]>(`/nodes/${nodeId}/coupons`),
  addNodeCoupon: (nodeId: number, body: { code: string; percent_off: number; max_uses: number; enabled: boolean; expires_at?: string | null }) =>
    request<Coupon>(`/nodes/${nodeId}/coupons`, { method: 'POST', body: JSON.stringify(body) }),
  updateNodeCoupon: (id: number, body: { percent_off?: number; max_uses?: number; enabled?: boolean; expires_at?: string | null; clear_expiry?: boolean }) =>
    request<Coupon>(`/coupons/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteNodeCoupon: (id: number) => request<null>(`/coupons/${id}`, { method: 'DELETE' }),
  // 兑换码（admin 发放 / 用户兑换）
  adminGiftCards: () => request<GiftCard[]>('/admin/gift-cards'),
  addGiftCards: (body: { code?: string; amount_cents: number; count?: number; expires_at?: string | null }) =>
    request<GiftCard[]>('/admin/gift-cards', { method: 'POST', body: JSON.stringify(body) }),
  updateGiftCard: (id: number, body: { amount_cents?: number; expires_at?: string | null; clear_expiry?: boolean }) =>
    request<GiftCard>(`/admin/gift-cards/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteGiftCard: (id: number) => request<null>(`/admin/gift-cards/${id}`, { method: 'DELETE' }),
  // 节点防火墙（rfw）
  nodeFirewall: (nodeId: number) => request<NodeFirewall>(`/nodes/${nodeId}/firewall`),
  createFirewallRule: (nodeId: number, body: {
    priority?: number; enabled?: boolean; direction: string; protocol: string;
    port_start?: number; port_end?: number; ip_type?: string; ip?: string; countries?: string[]; action: string;
  }) => request<FirewallRule>(`/nodes/${nodeId}/firewall`, { method: 'POST', body: JSON.stringify(body) }),
  deleteFirewallRule: (nodeId: number, ruleId: number) =>
    request<null>(`/nodes/${nodeId}/firewall/${ruleId}`, { method: 'DELETE' }),
  // 交易市场（二手实例买卖）
  marketListings: () => request<MarketListing[]>('/market/listings'),
  myMarketListings: () => request<MarketListing[]>('/market/listings/mine'),
  createMarketListing: (body: { instance_id: number; price_cents: number }) =>
    request<MarketListing>('/market/listings', { method: 'POST', body: JSON.stringify(body) }),
  cancelMarketListing: (id: number) =>
    request<MarketListing>(`/market/listings/${id}`, { method: 'DELETE' }),
  buyMarketListing: (id: number) =>
    request<MarketListing>(`/market/listings/${id}/buy`, { method: 'POST' }),
  // 技术支持工单
  tickets: () => request<Ticket[]>('/tickets'),
  ticketUnread: () => request<TicketUnread>('/tickets/unread-count'),
  createTicket: (body: { instance_id: number; title: string; content: string }) =>
    request<Ticket>('/tickets', { method: 'POST', body: JSON.stringify(body) }),
  ticket: (id: number) => request<Ticket>(`/tickets/${id}`),
  replyTicket: (id: number, content: string) =>
    request<TicketReply>(`/tickets/${id}/reply`, { method: 'POST', body: JSON.stringify({ content }) }),
  resolveTicket: (id: number) => request<Ticket>(`/tickets/${id}/resolve`, { method: 'POST' }),
  reopenTicket: (id: number) => request<Ticket>(`/tickets/${id}/reopen`, { method: 'POST' }),
  // 个人信息
  changeLoginPassword: (old_password: string, new_password: string) =>
    request<null>('/user/password', { method: 'POST', body: JSON.stringify({ old_password, new_password }) }),
  bindEmail: (email: string) =>
    request<{ email: string; email_verified: boolean; code_sent: boolean; message?: string }>('/user/email', {
      method: 'POST',
      body: JSON.stringify({ email }),
    }),
  verifyEmail: (code: string) =>
    request<{ email: string; email_verified: boolean }>('/user/email/verify', { method: 'POST', body: JSON.stringify({ code }) }),
  // 平台设置（admin）
  settings: () => request<PlatformSettings>('/settings'),
  updateSettings: (body: Partial<PlatformSettings>) =>
    request<null>('/settings', { method: 'PUT', body: JSON.stringify(body) }),
}

export function terminalWsUrl(instanceId: number): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/api/v1/instances/${instanceId}/terminal?token=${encodeURIComponent(token ?? '')}`
}
