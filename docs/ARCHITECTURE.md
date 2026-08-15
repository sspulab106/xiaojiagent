# 架构设计

## 1. 总体架构

平台采用 **Master-Agent** 分布式架构：Master 不直接操作 Linux 驱动，所有虚拟化、
网络与资源操作由部署在每台宿主机（母鸡）上的 Agent 守护进程执行，Master 只负责
编排、认证、计费与聚合。

```
                     ┌─────────────────────────────┐
                     │  Web 控制台 (Vite + React)   │
                     └──────────────┬──────────────┘
                                    │ REST (fetch) / WebSocket (xterm)
                     ┌──────────────▼──────────────┐
                     │  Go Master (Gin)             │
                     │  · JWT 认证 + RBAC           │
                     │  · 套餐 / 配额 / 月租到期     │
                     │  · 实例编排 (调度到某节点)     │
                     │  · 流量月度结算 (计数器差分)   │
                     │  · 节点健康巡检 (30s 轮询)    │
                     │  · WebSocket 终端中继        │
                     └──────────────┬──────────────┘
                                    │ HTTP + Bearer Token（每节点独立令牌）
                     ┌──────────────▼──────────────┐
                     │  Host Agent (每台母鸡一个)    │
                     │  · Provider 抽象             │
                     │    - incus: LXD/Incus REST   │
                     │    - oci: Docker/Podman API  │
                     │  · iptables NAT (DNAT+MASQ)  │
                     │  · 端口池分配 (默认 20000-40000)│
                     │  · 宿主机资源采集 (/proc)     │
                     │  · WebSocket ↔ pty 终端      │
                     └─────────────────────────────┘
```

## 2. 模块职责

### Master (`master/`)

- `internal/config`：全部由环境变量驱动，`DB_DRIVER` 在 `sqlite`（开发，纯 Go 驱动
  免 CGO）与 `postgres`（生产）之间切换，业务代码零改动。
- `internal/model`：核心模型——`User`（含订阅到期时间与实例配额）、`Package`
  （套餐，创建实例时快照到 `Instance`）、`Node`（母鸡）、`Instance`（含流量月账
  与计数器基线）、`PortMapping`、`TrafficLog`（预留的日流量历史表）。
- `internal/service`：
  - `CreateInstance`：校验配额/套餐 → `PickNode`（在线节点中实例最少者）→
    生成名称 `u<uid>-<rand>` → 调 Agent 创建 → 落库 + 保存自动分配的 SSH 映射。
  - `Stats`：Agent 返回的 rx/tx 是**累计计数器**。Master 保存上次读数
    `LastRxBytes/LastTxBytes`，把差分累加进 `TrafficUsedUp/Down`；跨月自动清零，
    计数器回绕（容器重启/Agent 重启）安全跳过。
  - `NodeHealthLoop`：每 30s 探测各 Agent `/health`，刷新节点在线状态与硬件总量。
- `internal/handler`：`Terminal` 是唯一不在 JWT 中间件下的路由——浏览器 WebSocket
  无法携带自定义请求头，故用 `?token=<jwt>` 查询串传令牌，处理器内部手动校验后
  `SetUser` 注入上下文，再转发到 Agent 的终端 WebSocket。
- `internal/agentclient`：Master→Agent 的 HTTP 客户端，响应同样走统一 envelope。

### Agent (`agent/`)

- `internal/provider`：**Provider 抽象**是所有虚拟化后端的统一入口。
  - `incusProvider`：走 Incus/LXD `/1.0` REST API（unix socket），异步操作用
    `/1.0/operations/<id>/wait` 轮询。创建时通过 `limits.cpu/limits.memory`、
    root 设备 size 施加资源限制。
  - `ociProvider`：走 Docker 引擎 API（unix socket），对 Podman 的兼容 socket
    同样有效。内存/CPU 通过 `HostConfig.Memory/NanoCpus` 限制；OCI 无法直接
    限制磁盘，`Rebuild` 为删后重建。
- `internal/nat`：iptables 规则生成器。DNAT 做端口映射，POSTROUTING MASQUERADE
  让容器能访问外网。`ensureRule` 先 `-C` 查重再 `-A`，保证幂等。规则持久化在
  `state.json`，Agent 启动时 `RebuildAll()` 恢复。
- `internal/service/state.go`：单一 JSON 文件保存 NAT 规则与实例规格（重建时需要
  原始资源限制）。
- `internal/terminal`：WebSocket ↔ `creack/pty` 中继，前端发 `{type:"resize"}`
  JSON 调整终端尺寸，其余字节透传。

### Web (`web/`)

- `src/lib/api.ts`：薄封装 fetch client，统一处理 envelope 与 JWT；`/api` 由 Vite
  dev proxy 转发到 `:8080`。
- 页面：登录/注册、仪表盘（节点+实例概览）、实例管理（CRUD + 动作）、实例详情
  （实时统计每 5s 轮询 + NAT 端口表）、xterm 网页终端、管理后台（节点/用户/套餐）。
- 主题：Tailwind `darkMode: 'class'`，`<html>` 加 `.dark` 切换，偏好存 localStorage。

## 3. 数据流示例：创建一台小鸡

1. 前端 POST `/api/v1/instances {package_id, image}`（携带 JWT）。
2. Master 校验登录、配额、订阅到期、套餐启用。
3. `PickNode` 选中在线且实例最少的节点。
4. Master → Agent POST `/agent/v1/instances`（携带节点 Token）。
5. Agent `provider.Create` 创建容器并启动，分配 SSH 外部端口（22→hostPort），
   写入 iptables，规则入 `state.json`。
6. Agent 返回 `{name, status, ip, ssh_port}`。
7. Master 落库 Instance + SSH PortMapping，返回给前端。

## 4. 流量计量

- Agent 的 `Stats` 返回容器自启动以来累计 `rx_bytes/tx_bytes`（Incus 计数器 /
  Docker stats）。
- Master 差分累计进当月的 `TrafficUsedUp/Down`；月份变化时清零并重置基线。
- 超量后的限速/停机是后续迭代项（tc / 定时任务），当前仅做计量与展示。

## 6. 节点接入与安装脚本

管理员在「管理后台 → 节点 → 安装脚本」为一键生成 bash 安装脚本：

- 脚本从 `GET /downloads/agent` 下载 Agent 二进制（主控 `AGENT_BINARY_DIR`
  目录，公开端点），写入 `/opt/codetest-agent/config.json`（含
  token/web_password/socket/cli/wan_iface），并注册
  `codetest-agent.service` systemd 服务。
- 两种绑定方式：
  1. 脚本内嵌节点 Token → 安装即上线；
  2. 不内嵌 → 打开 `http://<host>:8792` 本地面板，输入 Web 密码并粘贴节点
     Token。Agent 的 `TokenSource` 让 Token 即时生效（无需重启），并持久化
     到 config.json（config.json 优先级高于环境变量）。
- 主控 `NodeHealthLoop` 每 30s 探测各节点 `/health`，将新接入机器标记为
  online 并采集硬件总量。

## 7. 已知限制与后续迭代

- NAT 目前基于 iptables；nftables、IPv6 直连（NDP）、tc 带宽 QoS 为后续方向。
- 计费仅到“到期时间”维度，未实现自动续费/超期关停的定时任务。
- OCI（Docker/Podman）后端按 `disk_mb` 设置 overlay `StorageOpt.size` 磁盘限额，
  但**依赖宿主存储文件系统启用 project quota**：容器存储目录（如
  `/var/lib/containers/storage`）所在分区需以 XFS `pquota` 或 ext4 `prjquota`
  挂载，且内核编译了 `CONFIG_QUOTA/CONFIG_QFMT_V2`；否则该选项被引擎静默忽略，
  `df -h` 容器内显示宿主机磁盘。Agent 启动时会检测并告警。实例磁盘越大，镜像层
  本身不占用配额，仅容器可写层（diff/upperdir）受限。
- 各模块未覆盖单元测试；`master` 可用 `httptest`，`agent` 可针对 `nat` 与
  `provider` 接口做 mock 测试。

## 8. 安全说明

- Agent 使用每节点独立的 Bearer Token 鉴权；Master 访问 Agent 的 URL 与 Token
  由管理员在后台录入，生产环境应通过内网/加密隧道传输。
- JWT 密钥 `JWT_SECRET` 必须修改默认值。
- iptables 操作要求 Agent 以 root 运行；容器创建关闭特权模式（`security.privileged`）。
