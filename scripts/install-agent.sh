#!/usr/bin/env bash
# =============================================================================
# 独角鲸云 Agent 一键安装 / 增量更新脚本
#
# 托管于 GitHub（sspulab106/xiaojiagent），支持任意远程主机（如阿根廷等海外
# 宿主机）通过一行命令安装：
#
#   # 方式一：bash <(curl ...)
#   MASTER_URL=http://<master>:8080 TOKEN=<节点Token> \
#     bash <(curl -fsSL https://raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh)
#
#   # 方式二：curl | bash
#   curl -fsSL https://raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh \
#     | MASTER_URL=http://<master>:8080 TOKEN=<节点Token> bash
#
# 可安全重复执行（增量更新）：Agent 二进制总是更新到最新；config.json 合并时
# 保留原安装方式（virt_type / socket_path / cli_bin / listen / data_dir / 端口段）
# 与凭据（token / web_password），不会破坏既有容器、NAT 规则或导致节点失联。
#
# 环境变量（均可覆盖）：
#   MASTER_URL          必填：Master 服务地址（Agent 二进制下载 / 节点注册）
#   TOKEN               节点 Token（可选：嵌入后自动回传注册，重跑时保留旧值）
#   WEB_PASSWORD        面板管理密码（可选：留空时保留既有值，首次安装自动生成）
#   VIRT_TYPE           虚拟化方式：oci（默认）| incus
#   LISTEN              监听地址（默认 :8792）
#   DATA_DIR            数据目录（默认 /opt/codetest-agent）
#   BIN_PATH            Agent 二进制路径（默认 /usr/local/bin/codetest-agent）
#   SOCKET_PATH         容器引擎 socket（默认按 VIRT_TYPE）
#   CLI_BIN             容器 CLI（默认 docker / incus）
#   REGISTRY_MIRROR     docker.io 镜像加速（默认 docker.1panel.live）
#   DATA_DISK_SIZE      XFS 数据盘大小（默认 20G）
#   SKIP_NDP_CHECK=1    跳过 subnet 模式的 NDP 自检
#   REPO_RAW            本仓库 raw 根（默认 https://raw.githubusercontent.com/sspulab106/xiaojiagent/main）
# =============================================================================
set -euo pipefail

: "${MASTER_URL:?请设置 MASTER_URL（Master 服务地址，如 http://1.2.3.4:8080）}"
MASTER_URL="$(echo "$MASTER_URL" | sed 's#/*$##')"
VIRT_TYPE="${VIRT_TYPE:-oci}"
LISTEN="${LISTEN:-:8792}"
DATA_DIR="${DATA_DIR:-/opt/codetest-agent}"
BIN_PATH="${BIN_PATH:-/usr/local/bin/codetest-agent}"
RFW_API="127.0.0.1:7734"                    # rfw 仅监听本地，由 agent 面板反代
REGISTRY_MIRROR="${REGISTRY_MIRROR:-docker.1panel.live}"
DATA_DISK="${DATA_DISK_SIZE:-20G}"          # 数据盘大小（XFS pquota），可环境变量覆盖
REPO_RAW="${REPO_RAW:-https://raw.githubusercontent.com/sspulab106/xiaojiagent/main}"

# 容器引擎默认值（按虚拟化方式）
if [ -z "${SOCKET_PATH:-}" ]; then
  if [ "$VIRT_TYPE" = "oci" ]; then
    SOCKET_PATH="/var/run/docker.sock"
    CLI_BIN="${CLI_BIN:-docker}"
  else
    SOCKET_PATH="/var/lib/incus/unix.socket"
    CLI_BIN="${CLI_BIN:-incus}"
  fi
fi

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"; }

echo "[1/8] 检测架构并准备目录..."
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="x86_64" ;;
  aarch64|arm64) ARCH="aarch64" ;;
  *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac
mkdir -p "$DATA_DIR"

echo "[2/8] 安装虚拟化运行时与工具（可选，失败不阻断）..."
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  if [ "$VIRT_TYPE" = "oci" ]; then
    apt-get update -qq >/dev/null 2>&1 || true
    apt-get install -y -qq podman xfsprogs iptables >/dev/null 2>&1 || true
  else
    apt-get update -qq >/dev/null 2>&1 || true
    apt-get install -y -qq incus xfsprogs >/dev/null 2>&1 || true
  fi
fi
# 兜底：使用已有 docker / incus 时不强制安装

echo "[3/8] 配置内核参数（BBR / 转发 / inotify / IPv6）..."
cat > /etc/sysctl.d/99-codetest.conf <<'SYSCTL'
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
fs.inotify.max_user_instances=524288
fs.inotify.max_user_watches=1048576
fs.inotify.max_queued_events=65536
kernel.pid_max=4194304
fs.file-max=2097152
fs.nr_open=2097152
vm.swappiness=100
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
net.ipv6.conf.default.forwarding=1
net.ipv6.conf.all.accept_ra=2
net.ipv6.conf.default.accept_ra=2
net.ipv6.conf.all.use_tempaddr=0
net.ipv6.conf.default.use_tempaddr=0
SYSCTL
sysctl -p /etc/sysctl.d/99-codetest.conf >/dev/null 2>&1 || true

echo "[4/8] 配置 XFS 数据盘（pquota：容器磁盘限额的基础）..."
setup_xfs_data() {
  local disk=/xfs_disk.img mount=/data
  if [ -f "$disk" ]; then
    mkdir -p "$mount"
    mountpoint -q "$mount" || mount -o defaults,pquota,loop,noatime "$disk" "$mount" 2>/dev/null || true
  else
    log "创建数据盘 $disk ($DATA_DISK)..."
    if command -v fallocate >/dev/null 2>&1; then
      fallocate -l "$DATA_DISK" "$disk" 2>/dev/null || truncate -s "$DATA_DISK" "$disk" 2>/dev/null || true
    else
      truncate -s "$DATA_DISK" "$disk" 2>/dev/null || true
    fi
    mkfs.xfs -f "$disk" >/dev/null 2>&1 || true
    mkdir -p "$mount"
    mount -o defaults,pquota,loop,noatime "$disk" "$mount" 2>/dev/null || true
  fi
  grep -q "$disk" /etc/fstab || echo "$disk $mount xfs defaults,pquota,loop,noatime 0 0" >> /etc/fstab || true
  # Direct IO 解决 loop 设备双重缓存导致的内存压力
  cat > /etc/udev/rules.d/99-loop-directio.rules <<EOF
ACTION=="add|change", SUBSYSTEM=="block", KERNEL=="loop*", ATTR{loop/backing_file}=="$disk", ATTR{loop/direct_io}="1"
EOF
  udevadm control --reload-rules && udevadm trigger 2>/dev/null || true
  local loop_dev
  loop_dev=$(mount 2>/dev/null | grep "$mount" | awk '{print $1}') || true
  if [[ "$loop_dev" == /dev/loop* ]]; then
    echo 1 > "/sys/block/${loop_dev#/dev/}/loop/direct_io" 2>/dev/null || true
  fi
}
if command -v mkfs.xfs >/dev/null 2>&1; then
  setup_xfs_data
else
  log "未找到 mkfs.xfs，跳过数据盘创建（容器磁盘限额将不可用）"
fi
if [ "$VIRT_TYPE" = "oci" ]; then
  mkdir -p /data/containers/storage
  cat > /etc/containers/storage.conf <<'STORAGE'
[storage]
driver = "overlay"
runroot = "/run/containers/storage"
graphroot = "/data/containers/storage"
STORAGE
fi

echo "[5/8] 下载/更新 Agent 二进制（增量：已存在则原子替换，不中断运行）..."
BIN_UPDATED=0
if curl -fsSL --max-time 180 -o "$BIN_PATH.new" "$MASTER_URL/downloads/agent" 2>/dev/null; then
  chmod +x "$BIN_PATH.new" 2>/dev/null || true
  if [ ! -x "$BIN_PATH" ]; then
    mv "$BIN_PATH.new" "$BIN_PATH" && BIN_UPDATED=1
  elif ! cmp -s "$BIN_PATH" "$BIN_PATH.new"; then
    # 原子替换：新版本落地后旧进程继续运行，最后统一重启服务；失败时回滚旧二进制。
    cp "$BIN_PATH" "$BIN_PATH.bak" 2>/dev/null || true
    if mv "$BIN_PATH.new" "$BIN_PATH" 2>/dev/null; then
      BIN_UPDATED=1
      log "Agent 已更新到新版本（旧版保留为 $BIN_PATH.bak）"
    else
      mv "$BIN_PATH.bak" "$BIN_PATH" 2>/dev/null || true
      log "Agent 更新失败，保留旧版本"
    fi
  else
    rm -f "$BIN_PATH.new"
    log "Agent 已是最新版本"
  fi
else
  rm -f "$BIN_PATH.new"
  if [ ! -x "$BIN_PATH" ]; then
    log "警告：下载 Agent 失败且本机无旧版本，安装将不完整"
  else
    log "警告：下载 Agent 失败，沿用本机已有版本"
  fi
fi

echo "[6/8] 检测 IPv6 模式（SNAT / subnet / none）..."
WAN_IFACE="$(ip route 2>/dev/null | awk '/default/ {print $5; exit}')" || true
: "${WAN_IFACE:=eth0}"
ipv6_plus_one() { python3 -c "import ipaddress; print(str(ipaddress.IPv6Address('$1') + 1))" 2>/dev/null; }
detect_ipv6() {
  local connected=0 endpoint
  for endpoint in ip.sb ipv6.icanhazip.com www.cloudflare.com www.google.com; do
    if curl -6 -s --max-time 5 "$endpoint" >/dev/null 2>&1; then connected=1; break; fi
  done
  if [ "$connected" = "0" ] && ! ip -6 route show default 2>/dev/null | grep -q .; then
    echo "none"; return 0
  fi
  local iface
  iface=$(ip -6 route show default 2>/dev/null | head -1 | awk '{print $5}')
  [ -n "$iface" ] || { echo "none"; return 0; }
  local ipv6_full addr prefix
  ipv6_full=$(ip -6 addr show dev "$iface" 2>/dev/null | grep "scope global" | grep -v "dynamic" | awk '{print $2}' | grep -v '/128$' | sort -t'/' -k2 -rn | head -1) || true
  [ -n "$ipv6_full" ] || ipv6_full=$(ip -6 addr show dev "$iface" 2>/dev/null | grep "scope global" | grep -v "dynamic" | awk '{print $2}' | sort -t'/' -k2 -rn | head -1) || true
  [ -n "$ipv6_full" ] || ipv6_full=$(ip -6 addr show dev "$iface" 2>/dev/null | grep "scope global" | awk '{print $2}' | sort -t'/' -k2 -rn | head -1) || true
  [ -n "$ipv6_full" ] || { echo "none"; return 0; }
  addr=$(echo "$ipv6_full" | cut -d'/' -f1) || true
  prefix=$(echo "$ipv6_full" | cut -d'/' -f2) || true
  # subnet 模式要求 ≤/64 且子网内多 IP 可独立出站；否则回退 SNAT
  if [ "$prefix" -le 64 ] 2>/dev/null; then
    local test_addr subnet_ok=0
    test_addr=$(ipv6_plus_one "$addr") || true
    if [ -n "$test_addr" ] && command -v python3 >/dev/null 2>&1; then
      if ip addr add "$test_addr/$prefix" dev "$iface" 2>/dev/null; then
        sleep 2
        for _ep in ip.sb ipv6.icanhazip.com; do
          if curl -6 --interface "$test_addr" -s --max-time 8 "$_ep" >/dev/null 2>&1; then subnet_ok=1; break; fi
        done
        ip addr del "$test_addr/$prefix" dev "$iface" 2>/dev/null || true
      fi
    fi
    if [ "$subnet_ok" = "1" ]; then echo "$iface|$addr|$prefix|$prefix"; else echo "$iface|$addr|snat|"; fi
  else
    echo "$iface|$addr|snat|"
  fi
}
IPV6_RESULT="$(detect_ipv6)"
IPV6_MODE="none"; IPV6_ADDR=""; IPV6_IFACE=""; IPV6_PREFIX=""; IPV6_SUBNET="fd00:10:91::/64"
NDP_IFACE=""; NDP_SUBNETS=""; NDP_NETWORK=""
if [ "$IPV6_RESULT" != "none" ]; then
  IPV6_IFACE="$(echo "$IPV6_RESULT" | cut -d'|' -f1)"
  IPV6_ADDR="$(echo "$IPV6_RESULT" | cut -d'|' -f2)"
  IPV6_MODE="$(echo "$IPV6_RESULT" | cut -d'|' -f3)"
  IPV6_PREFIX="$(echo "$IPV6_RESULT" | cut -d'|' -f4)"
  if [ "$IPV6_MODE" != "snat" ]; then
    IPV6_MODE="subnet"
    : "${IPV6_PREFIX:=64}"
    # 子网模式：在宿主机前缀内偏移 0xcafe 计算容器 /112 子网（与参考安装脚本一致），
    # 容器将获得该子网内的独立公网地址，由 Agent 内置 NDP 响应器对外通告。
    if command -v python3 >/dev/null 2>&1; then
      read -r CONTAINER_BASE CONTAINER_GW <<< "$(python3 - <<PYEOF
import ipaddress
net = ipaddress.IPv6Network('$IPV6_ADDR/$IPV6_PREFIX', strict=False)
base_int = int(net.network_address)
prefix = int('$IPV6_PREFIX')
offset = 0xcafe << (128 - prefix - 16) if prefix <= 112 else 0
container_int = (base_int | offset) & ~((1 << (128 - 112)) - 1)
container_net = ipaddress.IPv6Network(f'{ipaddress.IPv6Address(container_int)}/112', strict=False)
print(str(container_net.network_address), str(container_net.network_address + 1))
PYEOF
)"
      if [ -n "$CONTAINER_BASE" ]; then
        IPV6_SUBNET="${CONTAINER_BASE}/112"
        NDP_IFACE="$IPV6_IFACE"
        NDP_SUBNETS="${CONTAINER_BASE}/112"
        NDP_NETWORK="narwhal-net6"
        log "容器 IPv6 子网: ${IPV6_SUBNET} 网关: ${CONTAINER_GW}"
      fi
    fi
  fi
  log "IPv6 模式: $IPV6_MODE ($IPV6_ADDR via $IPV6_IFACE)"
  if [ "$IPV6_MODE" = "subnet" ]; then
    # 关闭 WAN 网卡 SLAAC 自动地址并开启 NDP 代理（Agent 启动时也会重放）
    if [ -n "$IPV6_IFACE" ]; then
      cat > /etc/sysctl.d/99-codetest-ipv6-subnet.conf <<EOF
net.ipv6.conf.${IPV6_IFACE}.autoconf=0
net.ipv6.conf.${IPV6_IFACE}.accept_ra_pinfo=0
net.ipv6.conf.${IPV6_IFACE}.accept_ra=2
net.ipv6.conf.${IPV6_IFACE}.proxy_ndp=1
EOF
      sysctl -p /etc/sysctl.d/99-codetest-ipv6-subnet.conf >/dev/null 2>&1 || true
    fi
  fi
else
  log "未检测到公网 IPv6，使用 IPv4 NAT"
fi

echo "[7/8] 安装 rfw eBPF 防火墙（预编译二进制）..."
install_rfw() {
  local triple
  case "$ARCH" in
    x86_64) triple="x86_64-unknown-linux-musl" ;;
    aarch64) triple="aarch64-unknown-linux-musl" ;;
    *) return 0 ;;
  esac
  if [ ! -x "$DATA_DIR/rfw" ]; then
    log "下载 rfw ($triple)..."
    curl -fsSL --max-time 180 -o "$DATA_DIR/rfw.new" "https://github.com/narwhal-cloud/rfw/releases/latest/download/rfw-$triple" || { log "rfw 下载失败，跳过安装"; return 0; }
    chmod +x "$DATA_DIR/rfw.new" && mv "$DATA_DIR/rfw.new" "$DATA_DIR/rfw"
  fi
  cat > /etc/systemd/system/rfw.service <<EOF
[Unit]
Description=RFW eBPF Firewall
After=network.target

[Service]
Type=simple
User=root
Environment=RUST_LOG=info
WorkingDirectory=$DATA_DIR
ExecStart=$DATA_DIR/rfw --iface $WAN_IFACE --api-addr $RFW_API
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now rfw 2>/dev/null || systemctl start rfw 2>/dev/null || true
  for _i in $(seq 1 20); do
    curl -fsSL --max-time 2 "$RFW_API/api/status" >/dev/null 2>&1 && { log "✓ rfw API 已就绪"; return 0; }
    sleep 0.5
  done
}
install_rfw || true

echo "[8/8] 增量合并配置（保留安装方式与 token/密码，不覆盖已有机器状态）并注册服务..."
# 增量更新：脚本可安全重复执行，用于升级 Agent 而不会破坏已有安装。
# 合并优先级：
#   1. 本次显式提供的键（TOKEN / WEB_PASSWORD，仅设置时写入）；
#   2. 既有 config.json 中的「安装方式」键（listen / virt_type / data_dir / 端口段 /
#      token / web_password / socket_path / cli_bin）——重跑脚本保留原值，避免换掉
#      虚拟化方式、监听端口或端口段导致已有容器与 NAT 规则失配；
#   3. 运行时探测键（master_url / wan_iface / rfw_addr / ipv6_* 等）以本次为准；
#   4. 其余既有键（新版本新增的配置）原样保留。
# 首次安装未设置 WEB_PASSWORD 时由脚本生成默认值。
if [ -z "${WEB_PASSWORD:-}" ]; then
  WEB_PASSWORD_DEFAULT="$(head -c8 /dev/urandom | od -An -tx1 | tr -d ' \n')" || WEB_PASSWORD_DEFAULT=""
fi
export CFG_WAN_IFACE="${WAN_IFACE}" CFG_MASTER_URL="${MASTER_URL}" CFG_RFW_ADDR="${RFW_API}" \
  CFG_IPV6_MODE="${IPV6_MODE}" CFG_IPV6_ADDR="${IPV6_ADDR}" CFG_IPV6_SUBNET="${IPV6_SUBNET}" \
  CFG_IPV6_IFACE="${IPV6_IFACE}" CFG_NDP_IFACE="${NDP_IFACE}" CFG_NDP_SUBNETS="${NDP_SUBNETS}" \
  CFG_NDP_NETWORK="${NDP_NETWORK}" \
  CFG_DATA_DIR="${DATA_DIR}" CFG_LISTEN="${LISTEN}" CFG_VIRT_TYPE="${VIRT_TYPE}" \
  CFG_SOCKET_PATH="${SOCKET_PATH}" CFG_CLI_BIN="${CLI_BIN}" CFG_WEB_PASSWORD_DEFAULT="${WEB_PASSWORD_DEFAULT:-}" \
  CFG_PORT_START="20000" CFG_PORT_END="40000" \
  TOKEN="${TOKEN:-}" WEB_PASSWORD="${WEB_PASSWORD:-}"
python3 - <<'PYEOF'
import json, os

path = os.path.join(os.environ.get("CFG_DATA_DIR", "/opt/codetest-agent"), "config.json")
old = {}
if os.path.exists(path):
    try:
        with open(path) as f:
            old = json.load(f)
    except Exception:
        old = {}

# 本次显式提供的键：TOKEN / WEB_PASSWORD 仅在设置时写入。
provided = {}
if os.environ.get("TOKEN"):
    provided["token"] = os.environ["TOKEN"]
if os.environ.get("WEB_PASSWORD"):
    provided["web_password"] = os.environ["WEB_PASSWORD"]

# 安装方式 / 凭据键：重跑脚本时保留既有值（本次显式提供的新值优先）。
method_keys = {"listen", "virt_type", "data_dir", "socket_path", "cli_bin",
               "port_start", "port_end", "token", "web_password"}

def env(k, default=""):
    return os.environ.get(k, default)

# 运行时探测的默认值（首次安装 / 环境变化时使用）。web_password 默认值仅在
# 既有配置缺失时生效，避免重跑脚本覆盖已设置的面板密码；socket_path / cli_bin
# 同理仅作首次安装默认。
defaults = {
    "listen": env("CFG_LISTEN", ":8792"),
    "virt_type": env("CFG_VIRT_TYPE", "oci"),
    "data_dir": env("CFG_DATA_DIR", "/opt/codetest-agent"),
    "web_password": env("CFG_WEB_PASSWORD_DEFAULT", ""),
    "socket_path": env("CFG_SOCKET_PATH", ""),
    "cli_bin": env("CFG_CLI_BIN", ""),
    "wan_iface": env("CFG_WAN_IFACE"),
    "master_url": env("CFG_MASTER_URL"),
    "port_start": int(env("CFG_PORT_START", "20000")),
    "port_end": int(env("CFG_PORT_END", "40000")),
    "rfw_addr": env("CFG_RFW_ADDR"),
    "ipv6_mode": env("CFG_IPV6_MODE"),
    "ipv6_addr": env("CFG_IPV6_ADDR"),
    "ipv6_subnet": env("CFG_IPV6_SUBNET"),
    "ipv6_iface": env("CFG_IPV6_IFACE"),
    "ndp_iface": env("CFG_NDP_IFACE"),
    "ndp_subnets": env("CFG_NDP_SUBNETS"),
    "ndp_network": env("CFG_NDP_NETWORK"),
}

new = dict(provided)
# 运行时探测键：以本次为准（master_url / ipv6 等随环境刷新）；显式提供的键优先。
for k, v in defaults.items():
    if k not in provided:
        new[k] = v
# 安装方式键：既有值优先（除非本次显式提供），避免换掉虚拟化方式/端口段。
for k in method_keys:
    if k not in provided and k in old:
        new[k] = old[k]
# 其余既有键（新版本新增配置等）原样保留，避免丢失。
for k, v in old.items():
    new.setdefault(k, v)
with open(path, "w") as f:
    json.dump(new, f, indent=2, ensure_ascii=False)
PYEOF
chmod 600 "$DATA_DIR/config.json"

# docker.io 镜像加速：让 debian/alpine 等预设镜像可直接拉取
if [ "$VIRT_TYPE" = "oci" ] && command -v podman >/dev/null 2>&1; then
  mkdir -p /etc/containers/registries.conf.d
  cat > /etc/containers/registries.conf.d/zz-mirror.conf <<MIRROR
[[registry]]
location = "docker.io"

[[registry.mirror]]
location = "${REGISTRY_MIRROR}"
MIRROR
fi
cat > /etc/systemd/system/codetest-agent.service <<EOF
[Unit]
Description=CodeBuddy Agent (NAT VPS)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=${BIN_PATH}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable codetest-agent >/dev/null 2>&1 || true
if [ "$BIN_UPDATED" = "1" ]; then
  # 新二进制已就绪：重启 agent 使其生效。agent 启动时会重建 NAT/IPv6 规则并
  # 补设容器重启策略，不触碰既有容器与数据（增量更新）。
  systemctl restart codetest-agent || true
  log "Agent 已重启以应用新版本"
else
  systemctl start codetest-agent >/dev/null 2>&1 || systemctl restart codetest-agent >/dev/null 2>&1 || true
fi

# journald 日志限制，防止高并发实例日志撑爆系统盘
if command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/journald.conf <<'JOURNAL'
[Journal]
SystemMaxUse=200M
RuntimeMaxUse=50M
SystemMaxFileSize=50M
RateLimitIntervalSec=10s
RateLimitBurst=1000
ForwardToSyslog=no
JOURNAL
  systemctl restart systemd-journald 2>/dev/null || true
fi

# ── NDP 响应器自检（subnet 模式自动执行）────────────────────────────────────
# 从仓库拉取 verify-ndp.sh，创建临时 IPv6 实例验证 GlobalIPv6Address 与
# ip -6 neigh proxy 条目，验证完自动清理；失败不影响安装结果。
SKIP_NDP_CHECK="${SKIP_NDP_CHECK:-0}"
NDP_TOKEN="$(python3 -c "import json;print(json.load(open('$DATA_DIR/config.json')).get('token',''))" 2>/dev/null || true)"
if [ "$SKIP_NDP_CHECK" = "1" ]; then
  echo "  SKIP_NDP_CHECK=1，跳过自动 NDP 自检（可稍后手动运行 verify-ndp.sh）"
elif [ "$IPV6_MODE" = "subnet" ] && [ -n "$NDP_TOKEN" ] && [ "$NDP_TOKEN" != "change-me" ]; then
  if curl -fsSL --max-time 60 -o "$DATA_DIR/verify-ndp.sh" "$REPO_RAW/scripts/verify-ndp.sh" 2>/dev/null; then
    chmod +x "$DATA_DIR/verify-ndp.sh"
    echo "→ NDP 响应器自检：创建临时 IPv6 实例验证 GlobalIPv6Address 与 ip -6 neigh proxy（镜像拉取可能需要数分钟）..."
    AGENT_UP=0
    for _i in $(seq 1 30); do
      if curl -fsS --max-time 2 -H "Authorization: Bearer $NDP_TOKEN" "http://127.0.0.1:${LISTEN#:}/agent/v1/health" >/dev/null 2>&1; then AGENT_UP=1; break; fi
      sleep 2
    done
    if [ "$AGENT_UP" = "1" ]; then
      bash "$DATA_DIR/verify-ndp.sh" "http://127.0.0.1:${LISTEN#:}" \
        || echo "  ⚠ NDP 自检未完全通过（临时实例已自动清理）。可稍后手动重跑：bash $DATA_DIR/verify-ndp.sh"
    else
      echo "  ⚠ Agent 健康接口未就绪，本次跳过自动自检（可稍后手动运行 verify-ndp.sh）"
    fi
  else
    echo "  ⚠ verify-ndp.sh 拉取失败，跳过自动 NDP 自检"
  fi
elif [ "$IPV6_MODE" = "subnet" ]; then
  echo "  ⚠ 未配置 Agent Token，跳过自动 NDP 自检（绑定 Token 后可手动运行 verify-ndp.sh）"
else
  echo "  IPv6 模式为 ${IPV6_MODE:-none}，无需 NDP 自检（仅 subnet 模式需要）"
fi

echo "安装完成"
IP="$(hostname -I 2>/dev/null | awk '{print $1}')" || true
: "${IP:=<服务器IP>}"
PORT="${LISTEN#:}"
if [ -n "${TOKEN:-}" ]; then
  # 自动回传节点地址与 IP（地址留空时服务端据此接入，地区按公网 IP 自动识别）
  PUB_IPV4="$(curl -fsSL --max-time 6 https://api.ipify.org 2>/dev/null || true)"
  PUB_IPV6="$(curl -fsSL --max-time 6 https://api6.ipify.org 2>/dev/null || true)"
  : "${PUB_IPV4:=$IP}"
  curl -fsSL --max-time 10 -X POST "$MASTER_URL/api/v1/nodes/register" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$TOKEN\",\"agent_addr\":\"http://$IP:$PORT\",\"host_ip\":\"$PUB_IPV4\",\"ipv6\":\"$PUB_IPV6\"}" >/dev/null 2>&1 || true
  echo " 已自动回传节点地址: http://$IP:$PORT"
fi
echo "=============================================="
echo " Agent 面板: http://$IP:$PORT"
echo " IPv6: ${IPV6_MODE:-none}${IPV6_ADDR:+ ($IPV6_ADDR)}${IPV6_SUBNET:+ 容器子网 $IPV6_SUBNET}"
if [ "$IPV6_MODE" = "subnet" ]; then echo " NDP 响应器: ${NDP_IFACE:-未配置} (${NDP_SUBNETS:-无})"; fi
if [ -x "$DATA_DIR/rfw" ]; then echo " rfw 防火墙: 已安装 (API $RFW_API)"; else echo " rfw 防火墙: 未安装"; fi
echo " 数据盘: /xfs_disk.img -> /data (XFS pquota)"
if [ -n "${TOKEN:-}" ]; then echo " 已内嵌节点 Token，服务端将自动识别本机。"
else echo " 请在浏览器打开上面面板，粘贴服务端的节点 Token 完成绑定。"; fi
echo " Web 管理密码: ${WEB_PASSWORD:-${WEB_PASSWORD_DEFAULT:-（未设置）}}"
echo "=============================================="
