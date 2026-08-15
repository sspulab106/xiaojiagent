# scripts/ — 运维脚本目录

存放宿主机 / 主控端的一键运维脚本。脚本均为 **Bash**，在 **Agent 宿主机**
（root）上运行，可与一键安装脚本联动。后续新增运维脚本（数据盘迁移、流量
对账、节点巡检、NDP 对账修复等）统一放置本目录，并遵循文末
「新增运维脚本约定」。

| 脚本 | 作用 | 运行位置 | 与安装脚本的关系 |
| --- | --- | --- | --- |
| `install-agent.sh` | Agent 一键安装/增量更新（GitHub 托管，`bash <(curl ...)` 一行安装） | Agent 宿主机 | 本体（主控生成的是调用它的薄包装） |
| `verify-ndp.sh` | Agent NDP 响应器端到端验证（IPv6 subnet 模式） | Agent 宿主机 | 安装脚本在 subnet 模式下自动拉取并执行 |

---

## install-agent.sh — Agent 一键安装 / 增量更新（GitHub 托管）

主控「安装脚本」生成的是一层**薄包装**，注入本节点的 `MASTER_URL / VIRT_TYPE /
TOKEN / WEB_PASSWORD` 等参数后，通过标准一行命令拉取本脚本执行：

```bash
# 方式一：bash <(curl ...)（推荐）
MASTER_URL=http://<master>:8080 TOKEN=<节点Token> \
  bash <(curl -fsSL https://raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh)

# 方式二：curl | bash
curl -fsSL https://raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh \
  | MASTER_URL=http://<master>:8080 TOKEN=<节点Token> bash
```

- 可直接在任意海外宿主机（如阿根廷节点）执行，无需先克隆仓库；Agent 二进制
  从 `MASTER_URL/downloads/agent` 拉取，安装完成后带 Token 时自动回传注册。
- **可安全重复执行**（增量更新）：二进制原子替换（失败回滚、旧版留 `.bak`）；
  `config.json` 合并时保留原安装方式（`virt_type / socket_path / cli_bin /
  listen / data_dir / 端口段`）与凭据（`token / web_password`），只刷新运行时
  探测键（`master_url / wan_iface / ipv6_* / rfw_addr`）；rfw 防火墙、journald
  限制、IPv6 检测等幂等安装，不触碰既有容器与数据。
- 参数均可用环境变量覆盖：`MASTER_URL`（必填）、`TOKEN`、`WEB_PASSWORD`、
  `VIRT_TYPE(oci|incus)`、`LISTEN`、`DATA_DIR`、`BIN_PATH`、`SOCKET_PATH`、
  `CLI_BIN`、`REGISTRY_MIRROR`、`DATA_DISK_SIZE`、`SKIP_NDP_CHECK`、`REPO_RAW`。
- 主控可配置 `AGENT_INSTALL_SCRIPT_URL` 指向自建镜像或分叉仓库。

回归测试：`go test ./internal/handler/ -run TestInstallScript`（渲染包装 + 对
真实脚本的合并语义端到端验证）。

---

## verify-ndp.sh — NDP 响应器端到端验证

### 背景

IPv6 **subnet 模式**下，宿主机从其公网前缀内划出一个 `/112` 容器子网，每个
容器获得子网内**独立的公网 IPv6 地址**。上游路由器把该子网路由到宿主机后，
由 Agent 内置的 **NDP 响应器**（`internal/ndp`，通过 `ip -6 neigh add proxy`
逐地址通告）把对容器地址的邻居请求应答到宿主机，再经桥接/挂接转发进容器。

本脚本在宿主机上**创建一个临时 IPv6 实例**，依次验证 NDP 响应器是否真正
就绪、容器地址是否对外可达，验证完自动清理（`--keep` 可保留）。

### 用法

```bash
# 本机默认（Agent 面板 http://127.0.0.1:8792，Token 自动从 config.json 读取）
bash scripts/verify-ndp.sh

# 指定 Agent 地址 / 实例名 / 镜像
bash scripts/verify-ndp.sh http://10.0.0.1:8792
bash scripts/verify-ndp.sh http://10.0.0.1:8792 ndpv6 debian:13-slim

# 验证后保留实例与 proxy 条目（调试用）
bash scripts/verify-ndp.sh --keep

# 跳过外部可达性测试 / 自定义公网 IPv6 回显服务
bash scripts/verify-ndp.sh --no-echo
bash scripts/verify-ndp.sh --echo-url "http://ipv6.icanhazip.com https://ip.sb"
```

### 选项

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| 位置参数 1 | `http://127.0.0.1:8792` | Agent 地址 |
| 位置参数 2 | `ndptest-<时间戳>` | 实例名（已存在则复用，绝不删除） |
| 位置参数 3 | `debian:13-slim` | 创建镜像 |
| `--keep` | 关 | 验证后保留实例与 proxy 条目 |
| `--token XXX` | 读 `$DATA_DIR/config.json` | 覆盖 Agent Token |
| `--data-dir DIR` | `/opt/codetest-agent` | Agent 数据目录（config.json 所在） |
| `--image IMG` | `debian:13-slim` | 指定创建镜像（等价位置参数 3） |
| `--no-echo` | 关 | 跳过外部可达性测试 |
| `--echo-url URL...` | 见下 | 覆盖公网 IPv6 回显服务（空格分隔多个）；也支持环境变量 `ECHO_URLS` |

默认回显服务：`http://ipv6.icanhazip.com`、`http://ip.sb`、`https://api6.ipify.org`。

依赖：`curl`、`python3`、`iproute2`（`ip`）；无需 `jq`。

### 验证步骤（8 步）

1. **模式与响应器**：`ipv6_mode=subnet`、`ndp.enabled`、`ndp.iface` 非空
   （否则给出配置指引并退出）
2. **准备实例**：`POST /agent/v1/instances`（`ipv6:true`）；同名实例自动复用；
   镜像拉取超时后轮询等待
3. **GlobalIPv6 就绪**：轮询 `GET /agent/v1/instances/:name` 的 `ipv6` 字段
   （最长 60s），并用引擎 CLI **交叉验证**（`docker/podman inspect` 的
   `GlobalIPv6Address` / `incus list --format json`）
4. **子网归属**：python3 `ipaddress` 校验地址 ∈ 配置的 `ipv6_subnet`（/112）
5. **内核 NDP 代理**：`net.ipv6.conf.<ndp_iface>.proxy_ndp = 1`；且
   `ip -6 neigh show proxy dev <ndp_iface>` 含该地址（词边界匹配，防
   `::cafe:2` 误配 `::cafe:20`）
6. **Agent 跟踪集**：健康数据 `ndp.proxied` 包含该地址
7. **外部可达性（可选）**：`exec` 进容器执行
   `curl -6 --interface <容器地址> <回显服务>`，回显源地址 == 容器地址即证明
   **上游路由 → NDP 通告 → 容器挂接**全链路可用（含入站路径）；容器内无
   curl 时自动 `apt-get`/`apk` 安装（限时），全部回显无响应则 SKIP
8. **清理**：默认删除脚本创建的实例并等待 proxy 条目消失

### 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 全部检查通过 |
| `1` | 存在失败项（含回显源地址 ≠ 容器地址） |
| `2` | 用法错误 / 前置条件不满足（非 subnet 模式、缺少依赖、无有效 Token 等） |

判定语义：`[PASS]` / `[FAIL]` 计入通过/失败计数；`[SKIP]`（可选的外部可达性
测试因环境无法执行时出现）**不计入失败**，不影响退出码。

### 与一键安装脚本联动（自动执行）

安装脚本（`install-agent.sh`）在 `IPV6_MODE=subnet` 且 config.json 存在有效
Token 时，会从仓库自动拉取本脚本（`$REPO_RAW/scripts/verify-ndp.sh`，默认
`https://raw.githubusercontent.com/sspulab106/xiaojiagent/main`）并写入节点
`$DATA_DIR/verify-ndp.sh`（默认 `/opt/codetest-agent/`）：

- 拉取后 `chmod +x`，当 Agent 健康接口就绪（最长 60s）后**自动执行一次**
- 自检失败**不阻断安装**，仅打印 ⚠ 警告并提示手动重跑
- 设置环境变量 `SKIP_NDP_CHECK=1` 可跳过自动自检（镜像拉取耗时场景）

节点上手动静默重跑：

```bash
bash /opt/codetest-agent/verify-ndp.sh
```

### 管理后台 / 托管中心远程触发（节点自检）

主控管理后台的节点列表、托管中心的宿主机详情均提供「自检」按钮
（托管中心仅限机主本人，`/nodes/:id/selfcheck` 带所有权校验；管理后台走
`/admin/nodes/:id/selfcheck`）：

- 主控 `POST /admin/nodes/:id/selfcheck` → Agent `POST /agent/v1/selfcheck`
  异步启动（运行 `<DataDir>/verify-ndp.sh --data-dir <DataDir> http://127.0.0.1:<port>`，
  `VERIFY_NDP_SCRIPT` 可覆盖脚本路径，默认 `$DATA_DIR/verify-ndp.sh`）
- 前端每 2s 轮询 `GET /admin/nodes/:id/selfcheck/:runId` 回显输出，直至
  `done`/`failed`；单次运行上限 15 分钟，内存保留最近 20 次
- 节点离线或无脚本时给出明确错误（离线节点按钮自动置灰）
- 结果落库：启动时写「运行中」、终态写「通过/失败 + 退出码 + 时间 + 输出
  （截断 6KB）」到节点 `last_selfcheck` 字段，节点列表直接展示最近一次
  结果徽章（悬停可预览输出与时间）

### 常见问题排查

| 现象 | 排查方向 |
| --- | --- |
| 步骤 1 退出（非 subnet） | `config.json` 的 `ipv6_mode/ipv6_subnet/ndp_iface/ndp_subnets`；重启 Agent |
| 步骤 3 拿不到地址 | 实例是否已启动；复用实例可能未启用 IPv6；`incus network show narwhal-net6` / `podman network inspect` |
| 步骤 5 proxy 条目缺失 | `journalctl -u codetest-agent`；NDP 管理器是否启用（`ndp_iface`）；重启 Agent 触发对账 |
| 步骤 7 回显源地址不同 | 上游 ISP 是否允许子网内多 IP 出站（要求 ≤/64 前缀）；是否被 NAT66 |
| 步骤 7 全部无响应 | 容器出站受限或回显服务不可达；`--echo-url` 换服务再试 |

---

## 新增运维脚本约定

新脚本请遵循以下约定，保证与现有工具、安装脚本联动一致：

1. **放置与权限**：放在本目录，`chmod +x`；开头 `#!/usr/bin/env bash`。
2. **头部注释**：与 `verify-ndp.sh` 一致——脚本名 + 作用、背景说明、逐条
   检查/步骤清单、用法示例、选项表格、依赖清单、退出码语义。
3. **参数解析**：`set -u`；支持 `-h|--help`（`sed -n '2,/^# ===/p' "$0"`
   打印头部注释）；未知 `--*` 报错退出码 `2`；位置参数按 `POS_ARGS` 收集。
4. **Token 获取**：优先 `--token`，其次读 `$DATA_DIR/config.json` 的
   `token` 字段（`change-me` 视为无效）。
5. **退出码**：`0` 全部通过 / `1` 存在失败 / `2` 用法或前置错误。
6. **输出**：步骤用 `[N/M] 标题...`；单项用 `check <名称> <说明> 1|0`
   （`[PASS]`/`[FAIL]`）；可选检查用 `[SKIP]` 且不计入失败。
7. **可嵌入安装脚本时**（将被 `{{ .Script }}` 注入 `<<'NDPSH'` 引号 heredoc）：
   - 内容不得出现独立成行的 `NDPSH`；
   - 避免 `{{`/`}}` 序列（text/template 只解析外层，但保持零碰撞更稳）；
   - 涉及 docker/podman 的 Go 模板格式串（`{{range ...}}`）可安全保留，
     渲染测试会断言其原样透传；
   - 所有可能失败的命令用 `|| true` / `if` 保护，避免 `set -e` 下阻断安装；
   - 新增/修改后必须跑通
     `go test ./internal/handler/ -run TestInstallScriptRender`（渲染回归）。
8. **验证**：修改后至少 `bash -n` 语法检查；能用 mock 则对关键路径冒烟。
