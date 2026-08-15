import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import type { Instance, MarketListing, User } from '../lib/types'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  ConfirmDialog,
  Empty,
  Field,
  Input,
  Modal,
  Progress,
  Select,
  Tabs,
  cn,
  fmtDate,
  useToast,
} from '../components/ui'
import { CoinsIcon, StoreIcon } from '../components/icons'

// 折合剩余价值：剩余时间价值（按 30 天计费月比例）× 流量剩余率（0-100%）
function remainingValue(inst: Instance) {
  const monthMs = 30 * 24 * 3600 * 1000
  const paid = (inst.paid_cents && inst.paid_cents > 0 ? inst.paid_cents : inst.price_cents) || 0
  const now = Date.now()
  const expiry = inst.expires_at ? new Date(inst.expires_at).getTime() : now + monthMs
  let remaining = expiry - now
  if (remaining <= 0) return { value: 0, timeValue: 0, trafficRatio: 0, remainingDays: 0 }
  if (remaining > monthMs) remaining = monthMs
  const remainingDays = remaining / (24 * 3600 * 1000)
  const timeValue = Math.floor(paid * (remaining / monthMs))
  let trafficRatio = 100
  if (inst.traffic_gb > 0) {
    const total = inst.traffic_gb * 1024 ** 3
    const used = inst.traffic_used_up_bytes + inst.traffic_used_down_bytes
    if (used >= total) trafficRatio = 0
    else trafficRatio = Math.floor(((total - used) * 100) / total)
  }
  return { value: Math.floor((timeValue * trafficRatio) / 100), timeValue, trafficRatio, remainingDays }
}

function money(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`
}

function statusBadge(status: MarketListing['status']) {
  if (status === 'listed') return <Badge color="green" dot>在售中</Badge>
  if (status === 'sold') return <Badge color="blue" dot>已售出</Badge>
  return <Badge color="slate" dot>已下架</Badge>
}

export default function Market() {
  const [tab, setTab] = useState<'browse' | 'mine'>('browse')
  const [listings, setListings] = useState<MarketListing[]>([])
  const [mine, setMine] = useState<MarketListing[]>([])
  const [myInstances, setMyInstances] = useState<Instance[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionError, setActionError] = useState('')
  const toast = useToast()
  const [searchParams, setSearchParams] = useSearchParams()

  // 上架出售弹窗
  const [sellOpen, setSellOpen] = useState(false)
  const [sellInstanceId, setSellInstanceId] = useState<number>(0)
  const [sellPrice, setSellPrice] = useState('')
  const [selling, setSelling] = useState(false)
  const [sellError, setSellError] = useState('')

  // 购买确认弹窗
  const [buyTarget, setBuyTarget] = useState<MarketListing | null>(null)
  const [buying, setBuying] = useState(false)

  // 下架确认弹窗
  const [cancelTarget, setCancelTarget] = useState<MarketListing | null>(null)

  const load = useCallback(async () => {
    try {
      const [mk, my, insts, profile] = await Promise.all([
        api.marketListings(),
        api.myMarketListings(),
        api.instances(),
        api.profile(),
      ])
      setListings(mk)
      setMine(my)
      setMyInstances(insts)
      setUser(profile)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  // 支持 /market?sell=<instanceId> 自动打开上架弹窗并预选实例
  useEffect(() => {
    const sellParam = searchParams.get('sell')
    if (!loading && sellParam) {
      const id = Number(sellParam)
      openSell(id > 0 ? id : 0)
      setSearchParams({}, { replace: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, searchParams])

  // 已在售（含他人挂单不可重复上架，仅过滤自己的在售实例可上架）
  const listedIds = useMemo(() => new Set(mine.filter((l) => l.status === 'listed').map((l) => l.instance_id)), [mine])
  const sellable = useMemo(
    () =>
      myInstances.filter((inst) => {
        const expired = inst.expires_at ? new Date(inst.expires_at).getTime() <= Date.now() : true
        return !expired && !listedIds.has(inst.id)
      }),
    [myInstances, listedIds],
  )

  const openSell = (preSelectId = 0) => {
    setSellError('')
    if (preSelectId > 0) {
      const inst = sellable.find((i) => i.id === preSelectId)
      setSellPrice(inst ? money(remainingValue(inst).value) : '')
    } else {
      setSellPrice('')
    }
    setSellInstanceId(preSelectId)
    setSellOpen(true)
  }

  const sellInstance = sellable.find((i) => i.id === sellInstanceId)
  const value = sellInstance ? remainingValue(sellInstance) : null

  // 切换实例时同步默认价 = 折合剩余价值
  const pickSellInstance = (id: number) => {
    setSellInstanceId(id)
    const inst = sellable.find((i) => i.id === id)
    setSellPrice(inst ? money(remainingValue(inst).value) : '')
  }

  const submitSell = async () => {
    const cents = Math.round(Number(sellPrice) * 100)
    if (!sellInstanceId || !Number.isFinite(cents) || cents <= 0) {
      setSellError('请选择实例并填写有效售价')
      return
    }
    setSelling(true)
    setSellError('')
    try {
      const listing = await api.createMarketListing({ instance_id: sellInstanceId, price_cents: cents })
      setSellOpen(false)
      toast(`「${listing.instance?.display_name || listing.instance?.name || '实例'}」已上架出售`)
      await load()
    } catch (err) {
      setSellError(err instanceof ApiError ? err.message : '上架失败，请稍后重试')
    } finally {
      setSelling(false)
    }
  }

  const submitBuy = async (): Promise<boolean> => {
    if (!buyTarget) return false
    setBuying(true)
    setActionError('')
    try {
      const listing = await api.buyMarketListing(buyTarget.id)
      toast(`已购得实例「${listing.instance?.display_name || listing.instance?.name || ''}」`)
      setBuyTarget(null)
      await load()
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '购买失败，请稍后重试')
      return false
    } finally {
      setBuying(false)
    }
  }

  const submitCancel = async (): Promise<boolean> => {
    if (!cancelTarget) return false
    try {
      await api.cancelMarketListing(cancelTarget.id)
      toast('挂单已下架')
      setCancelTarget(null)
      await load()
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '下架失败，请稍后重试')
      return false
    }
  }

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-blue-600" />
      </div>
    )
  }

  if (error) {
    return <Card className="p-8 text-center text-sm text-red-600">{error}</Card>
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-slate-900">
            交易市场 <span className="text-sm font-normal text-slate-400">Marketplace</span>
          </h1>
          <p className="mt-0.5 text-sm text-slate-500">
            二手实例买卖：剩余时间、剩余流量折合剩余价值，低价捡漏他人未到期的小鸡
          </p>
        </div>
        <Button onClick={() => openSell()}>
          <CoinsIcon className="h-4 w-4" />
          上架出售
        </Button>
      </div>

      {actionError && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">{actionError}</div>
      )}

      <Tabs
        value={tab}
        onChange={(v) => setTab(v as 'browse' | 'mine')}
        options={[
          { key: 'browse', label: `交易市场（${listings.length}）` },
          { key: 'mine', label: `我的挂单（${mine.length}）` },
        ]}
      />

      {tab === 'browse' ? (
        <>
          {listings.length === 0 ? (
            <Empty
              text="暂无在售实例，点击右上角「上架出售」把你的小鸡挂上来"
              icon={<StoreIcon className="h-8 w-8" />}
            />
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {listings.map((l) => {
                const inst = l.instance
                const insufficient = user !== null && user.balance_cents < l.price_cents
                return (
                  <Card key={l.id} className="flex flex-col overflow-hidden">
                    {/* 实例头部 */}
                    <div className="flex items-start justify-between gap-2 border-b-2 border-black/10 px-4 py-3">
                      <div className="min-w-0">
                        <div className="truncate font-bold text-slate-900">{inst?.display_name || inst?.name || '-'}</div>
                        <div className="mt-0.5 truncate text-xs text-slate-400">
                          {inst?.node_name}
                          {inst?.node_region ? ` · ${inst.node_region}` : ''}
                          <span className="ml-1.5 font-mono">#{l.id}</span>
                        </div>
                      </div>
                      <Badge color="green" dot>在售</Badge>
                    </div>

                    <div className="flex-1 space-y-3 px-4 py-3">
                      {/* 配置 */}
                      <div className="flex items-center gap-2">
                        <div className="flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2 py-1 text-xs font-bold">
                          {inst?.cpu_cores}vCPU
                        </div>
                        <div className="flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2 py-1 text-xs font-bold">
                          {inst?.memory_mb} MB
                        </div>
                        <div className="flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2 py-1 text-xs font-bold">
                          {inst ? `${(inst.disk_mb / 1024).toFixed(1)} GB` : '-'}
                        </div>
                      </div>
                      <div className="truncate font-mono text-[11px] text-slate-400">{inst?.image}</div>

                      {/* 剩余时长 + 流量 */}
                      <div className="grid grid-cols-2 gap-2">
                        <div className="rounded-lg bg-slate-50 px-2.5 py-2">
                          <div className="text-[11px] text-slate-400">剩余时长</div>
                          <div className="mt-0.5 font-mono text-sm font-semibold text-slate-800">
                            {l.remaining_days != null ? `${l.remaining_days.toFixed(1)} 天` : '-'}
                          </div>
                        </div>
                        <div className="rounded-lg bg-slate-50 px-2.5 py-2">
                          <div className="text-[11px] text-slate-400">流量剩余</div>
                          <div className="mt-0.5 font-mono text-sm font-semibold text-slate-800">{l.traffic_ratio ?? 0}%</div>
                        </div>
                      </div>
                      <Progress value={l.traffic_ratio ?? 0} max={100} />

                      {/* 价值与售价 */}
                      <div className="flex items-center justify-between gap-2">
                        <div>
                          <div className="text-[11px] text-slate-400">折合剩余价值</div>
                          <div className="font-mono text-sm font-medium text-slate-500">
                            {l.value_cents != null ? money(l.value_cents) : '-'}
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="text-[11px] text-slate-400">售价</div>
                          <div className="font-mono text-lg font-extrabold text-emerald-600">{money(l.price_cents)}</div>
                        </div>
                      </div>
                    </div>

                    {/* 操作区 */}
                    <div className="flex items-center justify-between gap-2 border-t-2 border-black/10 bg-slate-50 px-4 py-2.5">
                      <span className="truncate text-xs text-slate-500">卖家：{l.seller_name || `用户#${l.seller_id}`}</span>
                      {l.seller_id === user?.id ? (
                        <Badge color="purple">我的挂单</Badge>
                      ) : (
                        <Button
                          size="sm"
                          variant="sage"
                          disabled={insufficient}
                          title={insufficient ? '余额不足' : undefined}
                          onClick={() => { setActionError(''); setBuyTarget(l) }}
                        >
                          购买
                        </Button>
                      )}
                    </div>
                  </Card>
                )
              })}
            </div>
          )}
          {user && (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border-[2.5px] border-black bg-white px-5 py-3 text-sm shadow-hard">
              <span className="text-slate-500">可用余额</span>
              <span className={cn('font-mono font-semibold', (user.balance_cents ?? 0) > 0 ? 'text-ink' : 'text-danger')}>
                {money(user.balance_cents ?? 0)}
              </span>
              <Link to="/recharge" className="text-brand underline">去充值</Link>
            </div>
          )}
        </>
      ) : (
        <Card>
          {mine.length === 0 ? (
            <Empty text="你还没有发布过挂单" icon={<StoreIcon className="h-8 w-8" />} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">实例</th>
                    <th className="px-5 py-3 font-medium">售价</th>
                    <th className="px-5 py-3 font-medium">剩余价值</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">上架时间</th>
                    <th className="px-5 py-3 font-medium">买家</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {mine.map((l) => (
                    <tr key={l.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3">
                        <div className="font-medium text-slate-900">{l.instance?.display_name || l.instance?.name || '-'}</div>
                        <div className="text-xs text-slate-400">{l.instance?.node_name}</div>
                      </td>
                      <td className="px-5 py-3 font-mono text-emerald-600">{money(l.price_cents)}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{l.value_cents != null ? money(l.value_cents) : '-'}</td>
                      <td className="px-5 py-3">{statusBadge(l.status)}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{fmtDate(l.created_at)}</td>
                      <td className="px-5 py-3 text-slate-500">
                        {l.status === 'sold' ? `用户#${l.buyer_id}` : '-'}
                      </td>
                      <td className="px-5 py-3 text-right">
                        {l.status === 'listed' && (
                          <Button size="sm" variant="outline" onClick={() => { setActionError(''); setCancelTarget(l) }}>
                            下架
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      {/* 上架出售弹窗 */}
      <Modal
        open={sellOpen}
        title="上架出售"
        subtitle="按剩余时间与剩余流量折合剩余价值，可自行调整售价"
        size="lg"
        onClose={() => { if (!selling) setSellOpen(false) }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setSellOpen(false)} disabled={selling}>取消</Button>
            <Button onClick={submitSell} disabled={selling || !sellInstanceId || !sellPrice}>
              {selling ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '确认上架'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="选择要出售的实例" hint="未到期且未在售的实例才能上架">
            <Select value={sellInstanceId} onChange={(e) => pickSellInstance(Number(e.target.value))}>
              <option value={0}>请选择实例…</option>
              {sellable.map((inst) => (
                <option key={inst.id} value={inst.id}>
                  {inst.display_name || inst.name}（{inst.cpu_cores}C/{inst.memory_mb}M · {inst.node_name}）
                </option>
              ))}
            </Select>
            {sellable.length === 0 && <p className="mt-1 text-xs text-slate-400">没有可上架的实例（需未到期且未在售）</p>}
          </Field>

          {sellInstance && value && (
            <div className="rounded-xl border-2 border-black/10 bg-slate-50 p-4">
              <div className="text-xs font-medium text-slate-500">剩余价值评估 Value Estimate</div>
              <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
                <div className="rounded-lg border-2 border-black bg-white p-2.5">
                  <div className="text-[11px] text-slate-400">剩余时长</div>
                  <div className="mt-1 font-mono text-sm font-semibold text-ink">{value.remainingDays.toFixed(1)} 天</div>
                </div>
                <div className="rounded-lg border-2 border-black bg-white p-2.5">
                  <div className="text-[11px] text-slate-400">流量剩余</div>
                  <div className="mt-1 font-mono text-sm font-semibold text-ink">{value.trafficRatio}%</div>
                </div>
                <div className="rounded-lg border-2 border-black bg-white p-2.5">
                  <div className="text-[11px] text-slate-400">时间价值</div>
                  <div className="mt-1 font-mono text-sm font-semibold text-ink">{money(value.timeValue)}</div>
                </div>
                <div className="rounded-lg border-2 border-black bg-mint p-2.5">
                  <div className="text-[11px] text-slate-500">折合剩余价值</div>
                  <div className="mt-1 font-mono text-sm font-extrabold text-ink">{money(value.value)}</div>
                </div>
              </div>
              <p className="mt-2 text-xs leading-5 text-slate-400">
                剩余价值 = 剩余时间价值 × 流量剩余率。参考原价 {money(sellInstance.price_cents)}/月，
                售价上限为原价（不允许抬价），成交后余额全额转给卖家，实例剩余时长/流量/端口/SSH 密码一并过户。
              </p>
            </div>
          )}

          <Field label="售价（USD）" hint="默认按折合剩余价值，上限为实例原价">
            <Input
              type="number"
              min={0.01}
              step={0.01}
              value={sellPrice}
              onChange={(e) => setSellPrice(e.target.value)}
              placeholder="0.00"
              disabled={!sellInstance}
            />
          </Field>
          {sellError && <p className="text-sm text-red-600">{sellError}</p>}
        </div>
      </Modal>

      {/* 购买确认弹窗 */}
      <ConfirmDialog
        open={!!buyTarget}
        title="确认购买"
        confirmText="确认购买"
        message={
          buyTarget ? (
            <div className="space-y-2">
              <div>
                购买实例「{buyTarget.instance?.display_name || buyTarget.instance?.name || '-'}」
                （{buyTarget.instance?.cpu_cores}C/{buyTarget.instance?.memory_mb}M · {buyTarget.instance?.node_name}）
              </div>
              <div className="text-xs text-slate-400">
                剩余时长约 {buyTarget.remaining_days?.toFixed(1)} 天，剩余价值 {buyTarget.value_cents != null ? money(buyTarget.value_cents) : '-'}
                ，流量剩余 {buyTarget.traffic_ratio ?? 0}%
              </div>
              <div className="flex items-baseline gap-1">
                成交价
                <span className="font-mono text-lg font-extrabold text-emerald-600">{money(buyTarget.price_cents)}</span>
                <span className="text-xs text-slate-400">将从余额中扣除</span>
              </div>
              <div className="flex items-center justify-between rounded-md bg-slate-50 px-3 py-2 text-xs">
                <span className="text-slate-500">可用余额</span>
                <span className={cn('font-mono font-semibold', (user?.balance_cents ?? 0) >= buyTarget.price_cents ? 'text-ink' : 'text-danger')}>
                  {money(user?.balance_cents ?? 0)}
                </span>
              </div>
            </div>
          ) : ''
        }
        onConfirm={submitBuy}
        onClose={() => setBuyTarget(null)}
      />

      {/* 下架确认弹窗 */}
      <ConfirmDialog
        open={!!cancelTarget}
        title="下架挂单"
        confirmText="确认下架"
        message={cancelTarget ? `确定将实例「${cancelTarget.instance?.display_name || cancelTarget.instance?.name || '-'}」的挂单下架吗？` : ''}
        onConfirm={submitCancel}
        onClose={() => setCancelTarget(null)}
      />
    </div>
  )
}
