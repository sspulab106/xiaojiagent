#!/usr/bin/env bash
# =============================================================================
# verify-ndp.sh — Agent NDP 响应器端到端验证脚本（IPv6 subnet 模式）
#
# 在 subnet 模式的 Agent 宿主机上创建一个 IPv6 实例，等待其拿到容器子网内的
# GlobalIPv6Address，然后逐一验证 NDP 响应器是否就绪：
#
#   1. Agent 处于 subnet 模式且 NDP 响应器已启用
#   2. 容器获得容器子网（ipv6_subnet）内的公网 IPv6 地址（GlobalIPv6Address）
#      —— 通过 Agent API 读取，并用引擎 CLI（docker/podman inspect / incus list）
#        交叉验证
#   3. 地址确实落在配置的 ipv6_subnet 内
#   4. 内核 proxy_ndp 已开启（net.ipv6.conf.<ndp_iface>.proxy_ndp = 1）
#   5. `ip -6 neigh` 存在对应 proxy 条目（NDP 响应器对外通告的关键）
#   6. Agent 健康数据 ndp.proxied 跟踪集包含该地址
#   7. 外部可达性测试（可选）：从容器内 curl --interface 访问公网 IPv6 回显
#      服务，回显源地址 == 容器公网地址即证明入站链路全通（上游路由 + NDP + 挂接）；
#      回显源地址不同则记为失败（该地址未被路由，入站不可用）
#   8. （默认）验证完成后删除实例，并确认 proxy 条目被清理
#
# 用法：
#   bash scripts/verify-ndp.sh [agent-url] [instance-name] [image]
#
#   bash scripts/verify-ndp.sh                            # 默认 http://127.0.0.1:8792
#   bash scripts/verify-ndp.sh http://10.0.0.1:8792
#   bash scripts/verify-ndp.sh http://10.0.0.1:8792 ndpv6 debian:13-slim
#
# 选项：
#   --keep          验证后保留实例与 proxy 条目（默认验证完删除）
#   --token XXX     覆盖 Agent Token（默认读 $DATA_DIR/config.json）
#   --data-dir DIR  覆盖 Agent 数据目录（默认 /opt/codetest-agent）
#   --image IMG     指定创建镜像（也可用第 3 个位置参数）
#   --no-echo       跳过外部可达性测试（默认自动执行；回显服务不可达时自动跳过）
#   --echo-url URL  覆盖公网 IPv6 回显服务（可空格分隔多个；默认 ipv6.icanhazip.com / ip.sb / api6.ipify.org）
#
# 依赖：curl、python3、iproute2（ip）；jq 不需要。
# 退出码：0 = 全部通过，1 = 存在失败项，2 = 用法/前置条件错误。
# =============================================================================
set -u

AGENT_URL="http://127.0.0.1:8792"
INSTANCE_NAME=""
IMAGE="debian:13-slim"
KEEP=0
DATA_DIR="/opt/codetest-agent"
TOKEN=""
ECHO=1   # 外部可达性测试（公网 IPv6 回显），--no-echo 关闭
ECHO_URLS="${ECHO_URLS:-http://ipv6.icanhazip.com http://ip.sb https://api6.ipify.org}"
POS_ARGS=()

usage() {
  sed -n '2,/^# ===/p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --keep) KEEP=1 ;;
    --token) TOKEN="${2:-}"; shift ;;
    --data-dir) DATA_DIR="${2:-}"; shift ;;
    --image) IMAGE="${2:-}"; shift ;;
    --no-echo) ECHO=0 ;;
    --echo-url) ECHO_URLS="${2:-}"; shift ;;
    -h|--help) usage; exit 0 ;;
    --*) echo "未知选项: $1" >&2; exit 2 ;;
    *) POS_ARGS+=("$1") ;;
  esac
  shift
done
[ "${#POS_ARGS[@]}" -ge 1 ] && AGENT_URL="${POS_ARGS[0]}"
[ "${#POS_ARGS[@]}" -ge 2 ] && INSTANCE_NAME="${POS_ARGS[1]}"
[ "${#POS_ARGS[@]}" -ge 3 ] && IMAGE="${POS_ARGS[2]}"
AGENT_URL="${AGENT_URL%/}"

for bin in curl python3 ip; do
  command -v "$bin" >/dev/null 2>&1 || { echo "缺少依赖: $bin" >&2; exit 2; }
done

# ---- Token：--token > config.json > 失败 ----
if [ -z "$TOKEN" ] && [ -f "$DATA_DIR/config.json" ]; then
  TOKEN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('token',''))" "$DATA_DIR/config.json" 2>/dev/null || echo "")
fi
if [ -z "$TOKEN" ] || [ "$TOKEN" = "change-me" ]; then
  echo "错误: 未找到有效 Agent Token（用 --token 指定，或确认 $DATA_DIR/config.json 已写入 token）" >&2
  exit 2
fi

if [ -n "$INSTANCE_NAME" ] && ! printf '%s' "$INSTANCE_NAME" | grep -Eq '^[A-Za-z0-9_-]{1,63}$'; then
  echo "错误: 实例名仅允许 [A-Za-z0-9_-] 且不超过 63 字符" >&2
  exit 2
fi

PASS=0; FAIL=0
check() { # check <名称> <说明> 1|0
  if [ "$3" = "1" ]; then
    PASS=$((PASS + 1)); echo "  [PASS] $1 — $2"
  else
    FAIL=$((FAIL + 1)); echo "  [FAIL] $1 — $2"
  fi
}
die() { echo "错误: $*" >&2; exit 1; }

api() { # api GET|POST|DELETE path [json-body] [max-time]
  local method="$1" path="$2" body="${3:-}" tmo="${4:-30}"
  if [ -n "$body" ]; then
    curl -fsS --max-time "$tmo" -X "$method" "$AGENT_URL$path" \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d "$body"
  else
    curl -fsS --max-time "$tmo" -X "$method" "$AGENT_URL$path" -H "Authorization: Bearer $TOKEN"
  fi
}

# ---------------------------------------------------------------------------
echo "[1/8] 检查 Agent IPv6 子网模式与 NDP 响应器..."
HEALTH=$(api GET /agent/v1/health 2>/dev/null) || die "无法访问 Agent: $AGENT_URL（请确认地址与 Token）"
MODE=$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['data'].get('ipv6_mode',''))" "$HEALTH" 2>/dev/null)
V6_SUBNET=$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['data'].get('ipv6_subnet',''))" "$HEALTH" 2>/dev/null)
NDP_ENABLED=$(python3 -c "import json,sys; d=json.loads(sys.argv[1])['data'].get('ndp') or {}; print('1' if d.get('enabled') else '0')" "$HEALTH" 2>/dev/null)
NDP_IFACE=$(python3 -c "import json,sys; d=json.loads(sys.argv[1])['data'].get('ndp') or {}; print(d.get('iface',''))" "$HEALTH" 2>/dev/null)

if [ "$MODE" != "subnet" ]; then
  echo "  当前 ipv6_mode=$MODE，本脚本仅适用于 subnet 模式。" >&2
  echo "  启用方式：config.json 设置 ipv6_mode=subnet、ipv6_subnet=<容器 /112 子网>、ndp_iface=<WAN 网卡>、ndp_subnets=<代理子网>，并重启 Agent。" >&2
  exit 2
fi
check "IPv6 subnet 模式" "ipv6_mode=$MODE, ipv6_subnet=$V6_SUBNET" "$([ "$MODE" = "subnet" ] && echo 1 || echo 0)"
check "NDP 响应器已启用" "ndp.enabled=$NDP_ENABLED, iface=${NDP_IFACE:-空}" "$NDP_ENABLED"
[ -z "$NDP_IFACE" ] && die "NDP 响应器未配置接口（config.json 的 ndp_iface 为空）"

# ---------------------------------------------------------------------------
echo "[2/8] 准备 IPv6 实例..."
if [ -z "$INSTANCE_NAME" ]; then
  INSTANCE_NAME="ndptest-$(date +%s)"
fi
CREATED=0
if api GET "/agent/v1/instances/$INSTANCE_NAME" >/dev/null 2>&1; then
  echo "  复用已有实例: $INSTANCE_NAME"
else
  BODY=$(python3 -c "import json,sys; print(json.dumps({'name':sys.argv[1],'image':sys.argv[2],'cpu_cores':1,'memory_mb':512,'disk_mb':5120,'swap_mb':0,'ipv6':True}))" "$INSTANCE_NAME" "$IMAGE")
  # 镜像拉取在 Agent 内同步进行，可能超过默认 30s；用 300s 上限，
  # 若 curl 仍超时则轮询等待实例出现（Agent 可能仍在后台创建）。
  if ! api POST /agent/v1/instances "$BODY" 300 >/dev/null 2>&1; then
    FOUND=0
    for _i in $(seq 1 30); do
      if api GET "/agent/v1/instances/$INSTANCE_NAME" >/dev/null 2>&1; then FOUND=1; break; fi
      sleep 2
    done
    [ "$FOUND" = "1" ] || die "创建实例失败：镜像拉取超时且实例未出现"
  fi
  CREATED=1
  echo "  已创建实例: $INSTANCE_NAME (镜像 $IMAGE)"
fi

# ---------------------------------------------------------------------------
echo "[3/8] 等待容器获得 GlobalIPv6 地址（最多 60s）..."
V6=""
for _i in $(seq 1 30); do
  INFO=$(api GET "/agent/v1/instances/$INSTANCE_NAME" 2>/dev/null || echo "")
  V6=$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['data'].get('ipv6',''))" "$INFO" 2>/dev/null || echo "")
  [ -n "$V6" ] && break
  sleep 2
done
check "容器 GlobalIPv6 地址就绪" "Agent 上报 ipv6 = ${V6:-（超时未获取到）}" "$([ -n "$V6" ] && echo 1 || echo 0)"
if [ -z "$V6" ] && [ "$CREATED" = "0" ]; then
  echo "  提示: 复用的实例可能未启用 IPv6（IPv4-only 或已停止）。建议换一个新实例名，或先启动该实例。"
fi

# 用引擎 CLI 交叉验证 GlobalIPv6Address（docker/podman inspect / incus list）
CLI_BIN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('cli_bin',''))" "$DATA_DIR/config.json" 2>/dev/null || echo "")
if [ -n "$V6" ] && [ -n "$CLI_BIN" ] && command -v "$CLI_BIN" >/dev/null 2>&1; then
  case "$CLI_BIN" in
    docker|podman)
      ENG_V6=$("$CLI_BIN" inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$v.GlobalIPv6Address}} {{end}}' "$INSTANCE_NAME" 2>/dev/null | tr ' ' '\n' | grep -v '^$' | head -1)
      check "引擎 GlobalIPv6Address 交叉验证" "$CLI_BIN inspect → ${ENG_V6:-空}" "$([ -n "$ENG_V6" ] && [ "$ENG_V6" = "$V6" ] && echo 1 || echo 0)"
      ;;
    incus|lxc)
      ENG_V6=$(python3 -c "
import json, subprocess, sys
out = subprocess.run([sys.argv[1],'list',sys.argv[2],'--format','json'], capture_output=True, text=True).stdout
addrs = set()
try:
    for it in json.loads(out):
        for eth in (it.get('state') or {}).get('network', {}).values():
            for a in eth.get('addresses', []):
                if a.get('family') == 'inet6' and not a.get('address','').lower().startswith('fe80'):
                    addrs.add(a['address'])
except Exception:
    pass
print(' '.join(sorted(addrs)))
" "$CLI_BIN" "$INSTANCE_NAME" 2>/dev/null | head -1)
      check "引擎 IPv6 交叉验证" "$CLI_BIN list → ${ENG_V6:-空}" "$([ -n "$ENG_V6" ] && echo 1 || echo 0)"
      ;;
  esac
fi

# ---------------------------------------------------------------------------
echo "[4/8] 校验地址落在容器子网内..."
if [ -n "$V6" ] && [ -n "$V6_SUBNET" ]; then
  IN_SUBNET=$(python3 -c "
import ipaddress, sys
print('1' if ipaddress.ip_address(sys.argv[1]) in ipaddress.ip_network(sys.argv[2], strict=False) else '0')
" "$V6" "$V6_SUBNET" 2>/dev/null || echo 0)
else
  IN_SUBNET=0
fi
check "地址在容器子网内" "$V6 ∈ $V6_SUBNET" "$IN_SUBNET"

# ---------------------------------------------------------------------------
echo "[5/8] 检查内核 NDP 代理配置..."
PROXY_NDP=$(sysctl -n "net.ipv6.conf.$NDP_IFACE.proxy_ndp" 2>/dev/null || echo 0)
check "proxy_ndp 已开启" "net.ipv6.conf.$NDP_IFACE.proxy_ndp = $PROXY_NDP" "$([ "$PROXY_NDP" = "1" ] && echo 1 || echo 0)"

NEIGH_OK=0
NEIGH_OUT=$(ip -6 neigh show proxy dev "$NDP_IFACE" 2>/dev/null || echo "")
# -w：按词匹配，避免 ::cafe:2 误匹配 ::cafe:20
if [ -n "$V6" ] && printf '%s' "$NEIGH_OUT" | grep -Fwq "$V6"; then
  NEIGH_OK=1
fi
check "ip -6 neigh proxy 条目存在" "dev $NDP_IFACE 含 $V6" "$NEIGH_OK"
[ "$NEIGH_OK" = "0" ] && [ -n "$NEIGH_OUT" ] && echo "  当前 proxy 条目: $(printf '%s' "$NEIGH_OUT" | tr '\n' '; ')"

# ---------------------------------------------------------------------------
echo "[6/8] 检查 Agent 健康数据跟踪集..."
IN_TRACKED=0; PROXIED_N=0
if [ -n "$V6" ]; then
  HEALTH2=$(api GET /agent/v1/health 2>/dev/null || echo "")
  PROXIED_N=$(python3 -c "import json,sys; d=json.loads(sys.argv[1])['data'].get('ndp') or {}; print(d.get('proxied_n',0))" "$HEALTH2" 2>/dev/null || echo 0)
  IN_TRACKED=$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])['data'].get('ndp') or {}
print('1' if sys.argv[2] in (d.get('proxied') or []) else '0')
" "$HEALTH2" "$V6" 2>/dev/null || echo 0)
fi
check "Agent 跟踪集包含地址" "ndp.proxied_n=$PROXIED_N，包含 $V6" "$IN_TRACKED"

# ---------------------------------------------------------------------------
echo "[7/8] 外部可达性测试：从容器内 curl --interface 验证公网入站链路（可选）..."
# 用公网 IPv6 回显服务验证完整双向链路：容器以自身公网地址为源发起访问，
# 回显返回的源地址 == 容器 GlobalIPv6Address，即证明上游路由 → 本机 NDP 通告
# → 容器挂接全链路可用（含从互联网主动连接容器地址的入站路径）。
if [ "$ECHO" = "0" ]; then
  echo "  [SKIP] --no-echo 已指定，跳过外部可达性测试"
elif [ -z "$V6" ]; then
  echo "  [SKIP] 未获取到容器 IPv6 地址（步骤 3 未通过），跳过外部可达性测试"
elif [ -z "$CLI_BIN" ] || ! command -v "$CLI_BIN" >/dev/null 2>&1; then
  echo "  [SKIP] 未找到引擎 CLI（${CLI_BIN:-空}），无法进入容器执行，跳过外部可达性测试"
else
  run_in() { # run_in <timeout_s> <cmd...>：在容器内限时执行命令
    local tmo="$1"; shift
    if command -v timeout >/dev/null 2>&1; then
      timeout "$tmo" "$CLI_BIN" exec "$INSTANCE_NAME" "$@" 2>/dev/null
    else
      "$CLI_BIN" exec "$INSTANCE_NAME" "$@" 2>/dev/null
    fi
  }
  # 先确认能进入容器（引擎守护进程/容器状态异常时给出明确提示）
  EXEC_OK=0
  run_in 5 true && EXEC_OK=1
  if [ "$EXEC_OK" = "0" ]; then
    echo "  [SKIP] 无法进入容器执行（$CLI_BIN exec 失败：容器未运行或引擎不可用），跳过外部可达性测试"
  else
  # 容器内优先用 curl；缺失时尝试 apt-get / apk 安装（限时 60s）
  CURL_READY=0
  if run_in 10 sh -c 'command -v curl >/dev/null 2>&1'; then
    CURL_READY=1
  elif run_in 60 sh -c 'command -v apt-get >/dev/null 2>&1 && { apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq curl >/dev/null 2>&1; }'; then
    CURL_READY=1
  elif run_in 60 sh -c 'command -v apk >/dev/null 2>&1 && apk add --no-cache curl >/dev/null 2>&1'; then
    CURL_READY=1
  fi
  if [ "$CURL_READY" = "0" ]; then
    echo "  [SKIP] 容器内无 curl 且安装失败，跳过外部可达性测试（可在容器内预装 curl 后重跑）"
  else
    V6_CLEAN="${V6%%%*}"
    ECHO_STATE="noresponse"; ECHO_NOTE=""
    for url in $ECHO_URLS; do
      # sh -c 通过 $1/$2 传参，避免嵌套引号问题
      RESP=$(run_in 25 sh -c 'curl -6 -s --max-time 15 --interface "$1" "$2" 2>/dev/null' sh "$V6_CLEAN" "$url" | tr -d '[:space:]')
      [ -z "$RESP" ] && continue
      KIND=$(python3 -c "
import ipaddress, sys

try:
    a = ipaddress.ip_address(sys.argv[1])
    b = ipaddress.ip_address(sys.argv[2])
except Exception:
    print('bad'); sys.exit(0)
print('same' if a == b else 'diff')
" "$RESP" "$V6_CLEAN" 2>/dev/null || echo bad)
      case "$KIND" in
        same) ECHO_STATE="pass"; ECHO_NOTE="$url → $RESP"; break ;;
        diff) ECHO_STATE="fail"; ECHO_NOTE="$url 回显 $RESP ≠ 容器地址 $V6_CLEAN（上游可能做 NAT 或地址未路由）"; break ;;
        *) : ;; # 非 IPv6 文本，视为无响应，尝试下一个
      esac
    done
    case "$ECHO_STATE" in
      pass) check "公网入站链路可达（回显验证）" "$ECHO_NOTE" "1" ;;
      fail) check "公网入站链路可达（回显验证）" "$ECHO_NOTE" "0" ;;
      *)    echo "  [SKIP] 所有回显服务均无响应（容器出站或上游 IPv6 受限）：$ECHO_URLS；未验证入站链路" ;;
    esac
  fi
  fi
fi

# ---------------------------------------------------------------------------
echo "[8/8] 清理..."
if [ "$KEEP" = "1" ]; then
  echo "  --keep 已指定，保留实例 $INSTANCE_NAME（其 proxy 条目也会保留）"
elif [ "$CREATED" = "1" ]; then
  api DELETE "/agent/v1/instances/$INSTANCE_NAME" >/dev/null 2>&1 \
    && echo "  已删除实例 $INSTANCE_NAME" \
    || echo "  实例删除失败（可能已不存在），proxy 条目将在下次对账清理"
  GONE=0
  for _i in $(seq 1 10); do
    if [ -n "$V6" ] && ! ip -6 neigh show proxy dev "$NDP_IFACE" 2>/dev/null | grep -Fwq "$V6"; then
      GONE=1; break
    fi
    sleep 1
  done
  check "删除后 proxy 条目已清理" "dev $NDP_IFACE 不再含 $V6" "$GONE"
else
  echo "  实例 $INSTANCE_NAME 为复用，未删除（避免影响既有实例）"
fi

# ---------------------------------------------------------------------------
echo "=============================================="
echo "结果: $PASS 项通过, $FAIL 项失败"
if [ "$FAIL" = "0" ]; then
  echo "✅ NDP 响应器验证通过：容器公网 IPv6 已就绪且内核 proxy 条目正常。"
  exit 0
else
  echo "❌ 存在失败项：请查看 Agent 日志（journalctl -u codetest-agent）、"
  echo "   config.json 的 ipv6_subnet/ndp_* 配置，以及上游路由器是否将 /64 路由到本机。"
  exit 1
fi
