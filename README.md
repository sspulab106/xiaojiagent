# 合租云控制台 (codetest)

一套多租户容器 / NAT VPS 合租管理平台：通过宿主机 Agent 对接 LXD / Incus /
Podman / Docker，将一台母鸡切成多台共享公网 IP 的轻量实例，并提供端口转发、
流量配额、网页终端与按月计费的后台。

## 架构

Master-Agent 分布式架构：

```
Web 控制台 (Vite + React)
        │ REST / WebSocket
Go Master (Gin) ──── 数据库 (SQLite 默认 / PostgreSQL 可选)
        │ HTTP + Bearer Token
   ┌────┴─────┐
Host Agent 1  Host Agent N
(LXD/Incus/Docker/Podman)
```

- `master/` — 主控端（Go + Gin）：认证/RBAC、套餐与配额、实例编排、流量月度统计、WebSocket 终端中继、节点健康巡检。
- `agent/` — 宿主机守护进程（Go）：统一 Provider 抽象（Incus REST API / OCI 引擎 API）、iptables NAT 端口映射、端口池分配、宿主机资源采集、pty 网页终端。
- `web/` — 前端（Vite + React + TS + Tailwind）：暗黑云原生风格控制台，含实例管理、NAT 映射、xterm 网页终端、管理后台。

详细设计见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，接口契约见 [docs/API.md](docs/API.md)。

## 快速开始

前置：Go 1.22+、Node 18+。

```sh
# 1. 拉取依赖（两个 Go 模块 + 前端）
(cd master && go mod tidy)
(cd agent && go mod tidy)
(cd web && npm install)

# 2. 启动主控（SQLite 默认，监听 :8080）
(cd master && go run ./cmd/master)

# 3. 启动 Agent（以 root 运行；对接 Incus，需安装 incus/lxd 与 iptables）
sudo AGENT_TOKEN=change-me VIRT_TYPE=incus sh -c 'cd agent && go run ./cmd/agent'

# 4. 启动前端（dev server :5173，/api 代理到 :8080）
(cd web && npm run dev)
```

首次访问 `http://localhost:5173`：注册的第一个账号自动成为管理员。之后在
「管理后台 → 节点」添加 Agent 地址与 Token，节点上线后即可创建实例。

### 常用环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ADDR` | `:8080` | 主控监听地址 |
| `DB_DRIVER` / `DB_PATH` / `DB_DSN` | `sqlite` / `data/master.db` | 生产切 `postgres` + DSN |
| `JWT_SECRET` | `dev-secret-change-me` | 生产环境务必修改 |
| `MASTER_PUBLIC_URL` | 空 | 外部可达地址，生成 Agent 安装脚本时用于下载二进制 |
| `AGENT_BINARY_DIR` | `data/bin` | 存放 `agent` 二进制的目录（`/downloads/agent` 提供下载） |
| `AGENT_LISTEN` / `AGENT_TOKEN` | `:8792` / `change-me` | Agent 监听与共享令牌 |
| `AGENT_WEB_PASSWORD` | 空 | 本地管理面板（`http://host:8792`）的密码 |
| `VIRT_TYPE` | `incus` | `incus`（LXD/Incus）或 `oci`（Docker/Podman） |
| `WAN_IFACE` / `PORT_START` / `PORT_END` | `eth0` / `20000` / `40000` | NAT 出口网卡与端口池 |

### 接入宿主机（两种方式）

「首页 → 接入宿主机」可一步完成：填宿主机名称/地址/虚拟化类型 → 自动生成
节点 Token 与一键安装脚本 → 在宿主机以 root 执行 → 节点自动上线（管理员也
可在「管理后台 → 节点 → 安装脚本」为已有节点生成脚本）：

1. **内嵌 Token**：脚本会包含该节点的 Token，目标机器以 root 执行后
   Agent 自动安装为 systemd 服务，服务端健康巡检会立刻将该节点标记为在线。
2. **不内嵌 Token**：安装完成后打开 `http://<宿主机IP>:8792` 的 Agent 管理
   面板，输入脚本末尾打印的 Web 管理密码并粘贴节点 Token，保存即完成绑定，
   服务端即可看到该机器上线并开始切分容器。

脚本会从主控 `GET /downloads/agent` 下载 Agent 二进制（需先将编译好的
`agent` 放入 `AGENT_BINARY_DIR`，并确保 `MASTER_PUBLIC_URL` 或面板访问地址
可被目标机器访问）；OCI 模式还会配置 docker.io 镜像加速，便于拉取预设镜像。

### 创建实例

创建实例时可自定义资源：内存（预设 64/128/256/512/1024/2048/4096 MB 或自定义）、
硬盘（1/2/5/10/20/40 GB 或自定义）、**默认开启 Swap（大小与内存一致）**，
并内置 debian(13-slim)、alpine(latest)、ubuntu(24.04) 三个一键镜像（也支持
自定义镜像地址）。资源上限以套餐为默认值，超出可覆盖。

### 本机快速验证（Podman）

本仓库已在 Ubuntu 20.04 + Podman 3.4 上完成端到端验证：容器创建（含
CPU/内存/Swap 限制）、iptables NAT 端口映射、流量统计、网页终端、节点在线巡检、
**自动开通 SSH**（安装 sshd + 随机 root 密码，面板展示 SSH 命令与密码）。
OCI 后端对 Docker 与 Podman 的兼容 socket 一视同仁；`docker.io` 不可达时，
镜像可写全镜像源地址，例如 `docker.1panel.live/library/ubuntu:22.04`。

> WSL 已知限制：WSL2 的 NAT 会丢弃容器"转发流量"的大数据包（宿主本身网络正常，
> 容器内 apt/下载大文件会卡），因此**本机 WSL 上自动安装 sshd 依赖的容器外网不可用**。
> 真实 VPS 无此问题。本机演示可用 `podman build --network=host` 预构建带 sshd 的镜像
> （构建走宿主网络栈绕过该问题），创建实例时选用该本地镜像即可。
>
> 容器内 SSH 需关闭 PAM（`UsePAM no`）：容器中 pam_loginuid/pam_keyinit 等会话
> 模块会失败，导致"密码验证通过后连接被关闭"。Agent 自动开通 SSH 已内置该配置，
> 本地预构建镜像也已烤入。

## 目录结构

```
├── go.work                 # 关联 master/agent 两个模块
├── master/                 # 主控端 Go 模块
│   ├── cmd/master/         # 入口
│   └── internal/
│       ├── config/         # 环境变量配置
│       ├── database/       # GORM 连接与迁移（sqlite/postgres）
│       ├── model/          # 数据模型（User/Package/Node/Instance/PortMapping）
│       ├── auth/           # JWT + RBAC 中间件
│       ├── response/       # 统一响应封装
│       ├── agentclient/    # Master→Agent HTTP 客户端
│       ├── service/        # 业务逻辑（编排/统计/节点巡检）
│       ├── handler/        # Gin 处理器（含 WebSocket 终端中继）
│       └── router/         # 路由注册
├── agent/                  # 宿主机 Agent Go 模块
│   ├── cmd/agent/          # 入口
│   └── internal/
│       ├── config/         # 配置
│       ├── provider/       # Provider 抽象 + Incus/OCI 实现
│       ├── nat/            # iptables NAT 管理 + 端口池
│       ├── service/        # 编排 + 状态持久化 + REST handlers
│       ├── terminal/       # WebSocket ↔ pty 网页终端
│       └── api/            # Token 鉴权路由
└── web/                    # Vite + React 前端
    └── src/
        ├── lib/            # API client + 类型
        ├── components/     # 布局 + UI 组件库
        └── pages/          # 登录/仪表盘/实例/详情/终端/管理后台
```
