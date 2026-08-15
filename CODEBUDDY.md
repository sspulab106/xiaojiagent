# CODEBUDDY.md

This file provides guidance to CodeBuddy Code when working with code in this repository.

## Project overview

A multi-tenant container / NAT VPS rental platform ("合租云控制台"). A central
Go (Gin) master orchestrates host agents that run on physical machines and
provision LXD/Incus/Podman/Docker containers with iptables NAT port forwarding,
traffic metering and a WebSocket web terminal. Frontend is Vite + React + TS +
Tailwind (dark cloud-native style).

Monorepo with two Go modules tied by `go.work` (`master/`, `agent/`) and a Node
frontend (`web/`). Requires Go 1.22+ and Node 18+.

## Commands

```sh
# ---- Backend (two Go modules) ----
(cd master && go mod tidy && go run ./cmd/master)   # master on :8080
(cd agent && go mod tidy && go run ./cmd/agent)     # agent on :8792 (root)

go vet ./master/... ./agent/...
gofmt -l master agent

# ---- Frontend ----
(cd web && npm install && npm run dev)              # dev server on :5173
(cd web && npm run build)                           # typecheck + build
```

Note: Go 1.26.5 is installed at `/usr/local/go/bin` and Node 18+ (v24) is
available in this environment; `web/node_modules` is already installed. The
module proxy `proxy.golang.org` is unreachable here — use `GOPROXY=https://goproxy.cn,direct`
when running `go mod tidy` / `go build`. The user's production/dev machines must
provide Go 1.22+ and Node 18+.

## Configuration (env vars)

Master: `ADDR` (:8080), `DB_DRIVER` (sqlite|postgres), `DB_PATH`, `DB_DSN`,
`JWT_SECRET` (must change from default), `TOKEN_TTL_HOURS`,
`MASTER_PUBLIC_URL` (externally reachable base URL, used in generated agent
install scripts), `AGENT_BINARY_DIR` (dir containing the `agent` binary served
at `/downloads/agent`; default `data/bin`).

Agent: `AGENT_LISTEN` (:8792), `AGENT_TOKEN`, `AGENT_WEB_PASSWORD` (guards the
local panel `http://host:8792/`), `VIRT_TYPE` (incus|oci),
`SOCKET`/`OCI_SOCKET` (unix socket path), `WAN_IFACE`, `PORT_START`/`PORT_END`
(port pool), `INCUS_POOL`/`INCUS_NETWORK`, `AGENT_DATA_DIR` (state.json +
config.json). Token can also be set later via the panel — it is persisted to
config.json and applied immediately without restart.

## Agent onboarding (two flows)

Admin generates a one-shot install script per node:
`POST /admin/nodes/:id/install-script` (see `web` Admin → 节点 → 安装脚本).

- With token embedded: the script installs the agent + systemd unit and the
  node comes online automatically.
- Without token: after install, open `http://<host>:8792/` and paste the node
  token (guarded by the web password printed by the script).

The script downloads the agent binary from `GET /downloads/agent`
(`master/internal/handler/install.go`).

## Architecture

Master-Agent model. The master never touches the host OS; every virtualization,
NAT and resource operation goes through an agent on the target node over HTTP
with a per-node bearer token.

- `master/cmd/master` + `master/internal/router` — entry point and route wiring.
  All routes live here; JWT + RBAC middleware, admin group, and the WebSocket
  terminal route (registered outside auth middleware because browsers can't set
  headers on WebSocket upgrades — the handler validates `?token=` manually).
- `master/internal/model` — GORM models. Instances snapshot package limits;
  traffic accounting keeps `last_rx_bytes/last_tx_bytes` baselines and diffs
  counters into monthly `traffic_used_up/down` (safe across restarts and month
  rollover). Adding a field here requires a matching AutoMigrate entry in
  `database.Open`.
- `master/internal/service` — business logic: `CreateInstance` (quota checks,
  `PickNode` least-loaded online node, SSH port mapping), `Stats` (monthly
  traffic diff), `NodeHealthLoop` (30s agent pings), delete/action orchestration.
- `master/internal/handler` — thin Gin handlers + `Terminal` WebSocket relay
  (browser ↔ agent bidirectional copy via gorilla/websocket). `decorate()`
  fills computed JSON fields (node/package names).
- `agent/internal/provider` — the virtualization abstraction. Implementations:
  `incus.go` (Incus/LXD /1.0 REST over unix socket, async op polling, resource
  limits via `limits.*`) and `oci.go` (Docker engine API; also covers Podman's
  compat socket). Add backends by implementing the `Provider` interface in
  `provider.go`. Disk limits: `oci.go` sends `HostConfig.StorageOpt.size` from
  `spec.DiskMB`, which the overlay driver enforces via project quotas — the
  storage filesystem must be XFS (`pquota`) or ext4 (`prjquota`) mounted, else
  the option is ignored and `df -h` shows the host disk. `quota.go` detects
  support (via `/info` DockerRootDir + `/proc/self/mounts`); the agent logs a
  warning at startup when quotas are unavailable.
- `agent/internal/nat` — iptables DNAT/MASQUERADE with `-C` idempotency checks
  and a port pool (`AllocatePort`). Rules persist to `state.json` and are
  re-applied on agent start (`RebuildNAT`).
- `agent/internal/terminal` — WebSocket ↔ `creack/pty` relay; JSON resize
  messages are detected heuristically, everything else is byte-passed.
- `agent/internal/api` — master REST API (token-authenticated) + a local
  management panel (`GET /`, `GET /admin/status`, `POST /admin/settings`) that
  updates the agent token via a dynamic `TokenSource` and persists it to
  config.json.
- `web/src/lib/api.ts` — the single API client; `web/src/lib/types.ts` mirrors
  the master JSON contract. Keep both in sync when changing the API. Vite dev
  proxy forwards `/api` (including ws) to `http://localhost:8080`.

## Conventions

- Unified response envelope `{code, message, data}` (see
  `master/internal/response`); handlers never return bare payloads.
- All master routes registered in `router.New`; entry points stay free of
  routing logic.
- The master→agent and agent→provider boundaries are interfaces; keep handlers
  thin and business logic in services.
- Full API contract: `docs/API.md`. Architecture notes: `docs/ARCHITECTURE.md`.
