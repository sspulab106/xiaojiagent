# API 契约

所有响应使用统一 envelope：

```json
{ "code": 0, "message": "ok", "data": ... }
```

`code == 0` 表示成功；非零 code 与 HTTP 状态码一致。错误示例：

```json
{ "code": 401, "message": "unauthorized", "data": null }
```

除登录/注册与终端外，所有接口需要请求头 `Authorization: Bearer <jwt>`。

## Master API（`/api/v1`）

### 认证

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/auth/login` | `{username, password}` → `{token, user}` |
| POST | `/auth/register` | `{username, password}` → `{token, user}`（首个账号自动为 admin） |

### 用户

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/user/profile` | 当前用户信息（含钱包字段 `balance_cents` / `hosting_total_cents` / `hosting_available_cents` / `hosting_frozen_cents`） |
| POST | `/user/recharge` | `{amount_cents}` 充值到账（演示环境即时到账，不产生真实扣款） |

### 公告

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/announcements` | 平台公告列表（控制面板） |

### 托管中心（用户宿主机）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/nodes` | 当前用户的宿主机列表（admin 返回全部），含 `instance_count` 与健康快照（mem/disk/load/uptime/net 速率/带宽） |
| POST | `/nodes` | 一键接入宿主机（同 `/admin/hosts`，创建节点并返回 `{node, token, script}`） |
| GET | `/nodes/:id` | 节点详情（拥有者可见 `token`），用于绑定 Agent / 资源监控 |

### 套餐 / 镜像

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/packages` | 套餐列表（普通用户仅返回启用的） |
| GET | `/images` | 预设镜像列表 `[{id, name, ref}]`（debian:13-slim / alpine:latest / ubuntu:24.04） |

### 实例

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/instances` | 实例列表（admin 返回全部，普通用户仅自己的） |
| POST | `/instances` | `{package_id, image, name?, memory_mb?, disk_mb?, swap_mb?}` → 创建实例 |
| GET | `/instances/:id` | 实例详情（含 `node_name/node_region/node_host_ip/package_name`） |
| POST | `/instances/:id/action` | `{action: start\|stop\|restart\|rebuild}` |
| POST | `/instances/:id/auto-renew` | `{enabled}` 切换自动续费 |
| POST | `/instances/:id/password` | `{password}` 修改容器 root 密码（经 Agent 执行 chpasswd，同步更新实例记录） |
| DELETE | `/instances/:id` | 删除实例（同时删除 NAT 映射） |
| GET | `/instances/:id/stats` | 实时统计（见下） |
| GET | `/instances/:id/ports` | NAT 端口映射列表 |
| POST | `/instances/:id/ports` | `{container_port, protocol?, host_port?}` 添加映射 |
| DELETE | `/ports/:id` | 删除映射 |
| GET | `/instances/:id/terminal?token=<jwt>` | WebSocket 网页终端（查询串携带 JWT） |

创建实例的资源参数：`memory_mb`/`disk_mb` ≤0 时用套餐默认值；`swap_mb`
为 null 时默认 **swap = 内存**，传 0 关闭 swap，传正值则自定义。OCI 后端通过
`HostConfig.MemorySwap` 施加 swap，Incus 后端通过 `limits.memory.swap` 开启。

**自动开通 SSH**：Agent 创建实例后会自动在容器内安装并启动 sshd
（apt/apk），生成随机 root 密码，返回给主控存入实例的 `ssh_password` 字段。
前端实例详情页展示 `ssh root@<宿主机IP> -p <端口>` 与密码。

`/instances/:id/stats` 响应：

```json
{
  "status": "running",
  "cpu_percent": 12.3,
  "memory_used_mb": 128,
  "memory_limit_mb": 512,
  "rx_bytes": 1024000,
  "tx_bytes": 2048000,
  "traffic_used_up_bytes": 1048576,
  "traffic_used_down_bytes": 2097152
}
```

### 管理后台（admin）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/admin/stats` | `{nodes, nodes_online, instances, users}` |
| GET | `/admin/nodes` | 节点列表 |
| POST | `/admin/nodes` | `{name, agent_addr, token, region}` 接入节点 |
| POST | `/admin/hosts` | 一键接入宿主机，见下 |
| DELETE | `/admin/nodes/:id` | 删除节点（节点上存在实例时拒绝） |
| POST | `/admin/nodes/:id/install-script` | 生成一键安装脚本，见下 |
| GET | `/admin/users` | 用户列表 |
| POST | `/admin/users/:id/extend` | `{days}` 续期订阅 |
| GET | `/admin/packages` | 全部套餐（含停用） |
| POST | `/admin/packages` | `{name, cpu_cores, memory_mb, disk_mb, traffic_gb, port_slots, ipv6, price_cents}` |
| DELETE | `/admin/packages/:id` | 删除套餐 |

## Agent API（`/agent/v1`，需 `Authorization: Bearer <agent-token>`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/health` | 宿主机状态：`{status, host_ip, host_cpu_percent, host_mem_used_mb, host_mem_total_mb, host_disk_used_mb, host_disk_total_mb, total_cores}` |
| GET | `/instances` | 实例列表 `[{name, status, ip}]` |
| GET | `/instances/:name` | 实例详情 |
| POST | `/instances` | `{name, image, cpu_cores, memory_mb, disk_mb, ipv6}` → `{name, status, ip, ssh_port}` |
| POST | `/instances/:name/action` | `{action: start\|stop\|restart\|rebuild}` |
| DELETE | `/instances/:name` | 删除实例及其全部 NAT 规则 |
| GET | `/instances/:name/stats` | `{status, cpu_percent, memory_used_mb, memory_limit_mb, rx_bytes, tx_bytes}` |
| POST | `/instances/:name/ports` | `{container_port, protocol?, host_port?}` → 规则（含分配的 `host_port` 与规则 `id`） |
| DELETE | `/ports/:id` | 删除 NAT 规则 |
| GET | `/instances/:name/terminal` | WebSocket 终端（pty 会话） |

### 节点安装脚本 / 一键接入宿主机

`POST /api/v1/admin/hosts`（admin，首页"接入宿主机"流程）——
创建节点并返回配套 Token 与安装脚本，一步完成：

```json
{ "name": "hkg-1", "agent_addr": "http://1.2.3.4:8792", "region": "香港",
  "virt_type": "incus | oci", "with_token": true, "web_password": "可选" }
```

→ `{ node, token, script }`。脚本会下载 Agent 二进制、配置 docker.io 镜像
加速（OCI 模式）、写 `config.json` 并安装 systemd 服务；装好后节点自动上线，
之后完全在网页管理容器/端口/流量。

`POST /api/v1/admin/nodes/:id/install-script`（admin）为已有节点生成脚本：

```json
{ "with_token": true, "web_password": "可选，留空自动生成",
  "virt_type": "incus | oci", "socket_path": "可选", "cli_bin": "可选" }
```

→ `{ "script": "#!/usr/bin/env bash ..." }`。Agent 二进制下载端点
`GET /downloads/agent`（公开）。

### Agent 本地管理面板（`http://host:8792`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/` | 面板 HTML（状态 + 绑定 Token 表单） |
| GET | `/admin/status` | 运行状态 JSON（含 `token_set`） |
| POST | `/admin/settings` | `{web_password, token}` 校验面板密码并绑定/更新节点 Token（即时生效并持久化到 config.json） |

### WebSocket 终端协议

浏览器 → Master：`/api/v1/instances/:id/terminal?token=<jwt>`；
Master 校验后中继到 Agent：`/agent/v1/instances/:name/terminal`（携带节点 Token）。

消息格式：

- 终端字节流：直接作为文本消息透传。
- 尺寸调整：`{"type":"resize","cols":80,"rows":24}`（JSON 文本消息，Agent 端
  启发式识别）。

**整机重启后的终端可靠性**：

- OCI（Podman/Docker）容器创建时带 `RestartPolicy=always`，Agent 启动时会对存量
  容器补设该策略——宿主机整机重启后实例像真 VPS 一样自动恢复。
- Master 中继到 Agent 的 WebSocket 拨号带**重试退避**（最多 5 次），并先检查节点
  是否在线（离线时直接提示“节点离线”，不再返回晦涩的 `unexpected EOF`）。
- 前端终端页**自动重连**（1s→2s→4s…封顶 8s），Agent 启动窗口内的短暂失败可自愈。
  重试**次数上限可配置**：默认 20 次，可通过 `/terminal/:id?max_retries=<n>` 覆盖
  （`0` = 无限重试），达到上限后停止并提示，避免宿主机长时间离线时无限重试。

**端到端验证（模拟宿主机重启）**：

- `agent/e2e/reboot_test.go`：在真实 Podman/Docker 引擎上走完整流程——真实 Agent
  API 创建实例 → 断言 `RestartPolicy=always` → 模拟重启（停止容器、Agent 进程
  退出并按启动序列重新拉起、执行引擎 boot hook `podman start --all --filter
  restart-policy=always`）→ 断言实例自动恢复运行 → WebSocket 终端重连成功。
  无引擎 socket 时自动跳过（`go test ./e2e/ -v`）。
- `master/internal/handler/terminal_e2e_test.go`：验证浏览器 → Master 中继链路——
  Agent 启动窗口内的拨号重试、Agent 重启后的浏览器重连、Agent 宕机时返回明确错误
  （“无法连接节点终端”，而非 `unexpected EOF`）。`go test ./internal/handler/ -run TestTerminal`。

## 数据模型摘要

- `user`：`username, role(admin|user), instance_quota, expires_at`
- `package`：`name, cpu_cores, memory_mb, disk_mb, traffic_gb, port_slots, ipv6, price_cents, enabled`
- `node`：`name, agent_addr, token, status(online|offline), region, host_ip, total_cores, total_memory_mb, total_disk_mb`
- `instance`：`name, user_id, node_id, package_id, image, status, ip, cpu_cores, memory_mb, disk_mb, traffic_gb, port_slots, expires_at, traffic_month, traffic_used_up, traffic_used_down, last_rx_bytes, last_tx_bytes`
- `port_mapping`：`instance_id, agent_rule_id, host_port, container_ip, container_port, protocol`

## 市场模式与计费（2026-08-07）

### 套餐上架 / 购买

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/packages` | 买家 | 仅返回**已上架**套餐（机主套餐要求节点在线），附 `node_name/node_region`；admin 返回全部 |
| GET | `/nodes/:id/packages` | 机主/admin | 某节点的全部套餐（含未上架） |
| POST | `/nodes/:id/packages` | 机主/admin | 创建节点套餐：`{name, images[], cpu_cores, memory_mb, disk_mb, traffic_gb, port_slots, ipv6, price_cents, listed}` |
| PUT | `/packages/:id` | 机主/admin | 更新套餐（可只传 `listed` 切换上架） |
| DELETE | `/packages/:id` | 机主/admin | 删除套餐（有实例则拒绝） |
| POST | `/admin/packages` | admin | 创建平台套餐（node_id=0），同字段 + `listed` |
| POST | `/instances` | 买家 | `{package_id, image, name?, coupon_code?}`——配置固定于套餐，镜像须在套餐 `images` 内（平台套餐可任意） |

### 优惠码（购买折扣，非充值返现）

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/nodes/:id/coupons` | 机主/admin | 节点优惠码列表 |
| POST | `/nodes/:id/coupons` | 机主/admin | `{code, percent_off, max_uses, enabled}` |
| DELETE | `/coupons/:id` | 机主/admin | 删除 |
| GET/POST/DELETE | `/admin/coupons...` | admin | 平台级优惠码（node_id=0） |
| GET | `/user/coupons` | 买家 | 可用优惠码 |

### 计费闭环

- 创建实例：余额扣减**实际支付额**（原价 − 优惠码折扣），记 `purchase` 流水；机主套餐购买时付款的 **90% 入机主托管余额**（平台抽 10%），记 `host_income` 流水；`expires_at = 创建 + 30 天`。
- **取消机器（取消/释放，替代原“删除”）**：默认**不退款**（释放机器、资源回收，记无退款）；仅当满足全部条件时返还**全部余额**：
  1. 所属套餐开启 `early_full_refund`（允许早期全额退款）；
  2. 实例开通（创建）**≤ 1 小时**；
  3. 已用流量 **≤ 1GB**。
  满足时全额退款回买家余额（记 `refund`），机主托管余额按 90% **全额回冲**（记 `host_refund`）；不满足则无任何退款。
- 余额不足（`balance_cents < 价格`）拒绝创建。
- `/user/recharge` 不再支持优惠码。

**取消接口**：`DELETE /instances/:id`（同原删除接口，语义变更）。前端「取消机器」二次确认弹窗会按上述规则提示“返还全部余额 $X”或“释放机器且不退款”。在售（交易市场挂单中）的实例仍禁止取消，需先下架。

### 兑换码 / 礼品卡

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET/POST/DELETE | `/admin/gift-cards` | admin | 发放（`{code?, amount_cents, count}`，批量自动生成 `GIFT-XXXX-XXXX`）/列表/删除（已兑换不可删） |
| POST | `/user/redeem` | 用户 | `{code}` 兑换入账（金额 +10% 托管余额），记 `recharge` 流水；重复兑换/过期拒绝 |

### 实时监控与重装

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/nodes/:id/stats` | 机主/admin | 实时代理 agent 健康 + 每容器使用率：`{host:{...}, vms:[{name,status,cpu_percent,mem_used_mb,mem_limit_mb}]}` |
| POST | `/instances/:id/action` | 属主 | rebuild 支持 `{action:"rebuild", image:"..."}` 换镜像重装；SSH 密码保持不变、端口映射保留（agent 端重建后自动重装 sshd） |

### 二手实例交易市场（按剩余价值买卖未到期小鸡）

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/market/listings` | 登录用户 | 在售挂单列表（自动过滤已过期/实例已消失的），每条含内嵌实例与剩余价值评估 |
| GET | `/market/listings/mine` | 登录用户 | 我的全部挂单（含已售/已下架历史） |
| POST | `/market/listings` | 属主 | 上架：`{instance_id, price_cents}`；售价下限 1 分，上限为实例原支付价 |
| DELETE | `/market/listings/:id` | 卖家/admin | 下架在售挂单 |
| POST | `/market/listings/:id/buy` | 买家 | 购买挂单：扣买家余额、全额入卖家余额、实例过户（`user_id` 变更） |

**剩余价值公式**：`剩余价值 = 剩余时间价值 × 流量剩余率`，其中

- 剩余时间价值 = 实际支付价 × 剩余时长 / 30 天（`expires_at` 与当前时间差，上限 30 天）；
- 流量剩余率 = 当月流量配额剩余比例（`traffic_gb == 0` 视为无限 = 100%）。

每条挂单响应额外返回 `seller_name / value_cents / time_value_cents / traffic_ratio / remaining_days / expired / instance`（内嵌已装饰实例：节点/地区/配置/流量用量/到期时间）。

**成交规则**：实例过户后剩余时长、已用流量、端口映射与 SSH 密码全部保留；实例 `paid_cents` 更新为成交价，后续删除退款按成交价比例计算（防止低买高退）；卖家余额入账全额成交价（平台不抽成）；在售实例禁止删除（需先下架）；成交价与余额校验均在事务内原子执行，防并发重复购买。

## Agent 面板（新增）

- `GET /admin/status`：新增 `host` 实时资源快照（CPU/内存/磁盘/负载/网络/容器数），面板每 5 秒刷新。
- `POST /admin/update`：`{web_password}` 从 `AGENT_MASTER_URL` 拉取最新二进制替换并 `systemctl restart codetest-agent`（安装脚本已在 config.json 写入 `master_url`）。

## 数据模型摘要（市场模式更新）

- `package`：新增 `node_id`（0=平台）、`user_id`、`images`（逗号分隔镜像 ref）、`listed`（上架）、`early_full_refund`（允许早期全额退款，见计费闭环）、`created_at`
- `coupon`：新增 `node_id`、`user_id`；语义 = 购买折扣
- `instance`：新增 `paid_cents`（实际支付）
- `gift_card`：`code, amount_cents, status(issued|redeemed), created_by, redeemed_by, redeemed_at, expires_at`

## 技术支持工单（2026-08-15）

每个用户都可针对**自己名下的实例**提交工单，工单自动路由到实例所在宿主机的
技术支持页面；平台管理员可看到全部工单。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/tickets` | 登录 | 自己的工单；机主额外看到路由到其节点的工单；admin 全部 |
| GET | `/tickets/unread-count` | 登录 | `{node_unread, user_unread}`：机主/管理员看 node_unread，用户看 user_unread |
| POST | `/tickets` | 登录 | 提工单（`instance_id/title/content`，必须是自己名下的实例） |
| GET | `/tickets/:id` | 可见者 | 详情 + 完整回复；读取自动标记本方已读 |
| POST | `/tickets/:id/reply` | 可见者 | 回复，回复后对方未读 +1（已解决不可回复） |
| POST | `/tickets/:id/resolve` | 机主/admin/发起人 | 标记已解决（从未读气泡消失） |
| POST | `/tickets/:id/reopen` | 可见者 | 重新打开已解决的工单 |

未读规则：宿主机/管理员回复 → 用户未读；用户回复 → 宿主机未读；自己提给自己
节点的工单不计入未处理。前端侧边栏「技术支持」带未读气泡（30s 轮询）。

## 个人信息与平台设置（2026-08-15）

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| POST | `/user/password` | 登录 | 修改登录密码（校验旧密码） |
| POST | `/user/email` | 登录 | 绑定邮箱；平台开启邮箱验证时发送验证码 |
| POST | `/user/email/verify` | 登录 | 输入邮箱中的验证码完成验证 |
| GET | `/settings` | admin | 平台设置（SMTP 发件邮箱、邮箱验证开关、Cloudflare 人机验证预留） |
| PUT | `/settings` | admin | 保存设置；`smtp_pass` 留空保持不变，未知键忽略 |

## 实例状态同步与增量安装（2026-08-15）

- **状态同步**：Master 后台每 30s 调用各在线节点 Agent 校准实例状态与 IP；整机重启后
  未恢复的容器会被标记 `offline`（不再一直显示“运行中”）；节点离线的实例在列表页
  即时显示离线。
- **增量安装脚本**：`/admin/nodes/:id/install-script` 生成的脚本可安全重复执行——
  Agent 二进制总是下载并原子替换（失败回滚，旧版保留 `.bak`），仅当二进制更新时
  才重启 Agent；`config.json` 用 Python 合并，**保留原安装方式与凭据**：
  `virt_type / socket_path / cli_bin / listen / data_dir / port_start / port_end /
  token / web_password` 重跑时一律保留原值（token/面板密码仅当重新生成时显式
  覆盖，`socket_path`/`cli_bin` 属安装方式永不覆盖），运行时探测键
  （`master_url / wan_iface / ipv6_* / rfw_addr`）随本次刷新，新版本新增的未知键
  原样保留；rfw 防火墙、journald 限制、IPv6 检测等组件幂等安装，不触碰既有容器
  与数据。
