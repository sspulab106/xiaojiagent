import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError } from '../lib/api'
import type { Transaction, User } from '../lib/types'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Input,
  cn,
  fmtDate,
  useToast,
} from '../components/ui'
import { CoinsIcon, CreditCardIcon, SearchIcon, SendIcon, ShieldCheckIcon, WalletIcon } from '../components/icons'

const PAY_METHODS = [
  { id: 'card', cn: '信用卡', name: 'Credit Card', desc: 'Visa / Mastercard', icon: CreditCardIcon },
  { id: 'alipay', cn: '支付宝', name: 'Alipay', desc: '即时到账', icon: WalletIcon },
  { id: 'wechat', cn: '微信支付', name: 'WeChat', desc: '扫码支付', icon: SendIcon },
  { id: 'crypto', cn: '加密货币', name: 'Crypto', desc: 'USDT / TRC20', icon: CoinsIcon },
]

const QUICK_AMOUNTS = [10, 50, 100, 500]

const TYPE_FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'purchase', label: '购买' },
  { key: 'refund', label: '退款' },
  { key: 'recharge', label: '充值' },
  { key: 'host_income', label: '收益' },
]

const POSITIVE_TYPES = new Set(['recharge', 'refund', 'host_income'])
const TYPE_LABEL: Record<string, string> = {
  recharge: '充值',
  purchase: '购买',
  refund: '退款',
  host_income: '机主收益',
  host_refund: '收益回冲',
  admin_adjust: '管理员调整',
}

export default function Recharge() {
  const toast = useToast()
  const [user, setUser] = useState<User | null>(null)
  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [method, setMethod] = useState('alipay')
  const [amount, setAmount] = useState('10')
  const [query, setQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  // 兑换码（礼品卡）
  const [redeemCode, setRedeemCode] = useState('')
  const [redeeming, setRedeeming] = useState(false)
  const [redeemMsg, setRedeemMsg] = useState<{ ok: boolean; text: string } | null>(null)

  const load = useCallback(async () => {
    try {
      const [u, txs] = await Promise.all([api.profile(), api.transactions()])
      setUser(u)
      setTransactions(txs)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const amountNum = Number(amount)
  const validAmount = !isNaN(amountNum) && amountNum >= 1

  const filtered = useMemo(() => {
    return transactions.filter((t) => {
      const matchType = typeFilter === 'all' || t.type === typeFilter
      const q = query.trim().toLowerCase()
      const matchQuery = q === '' || t.description.toLowerCase().includes(q) || t.created_at.toLowerCase().includes(q)
      return matchType && matchQuery
    })
  }, [transactions, query, typeFilter])

  const submit = async () => {
    if (!validAmount) return
    setSubmitting(true)
    setError('')
    try {
      const u = await api.recharge(Math.round(amountNum * 100))
      setUser(u)
      setAmount('')
      toast('充值成功，余额已更新')
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '充值失败，请稍后重试')
    } finally {
      setSubmitting(false)
    }
  }

  const redeem = async () => {
    const code = redeemCode.trim()
    if (!code) return
    setRedeeming(true)
    setRedeemMsg(null)
    try {
      const u = await api.redeemGiftCard(code)
      setUser(u)
      setRedeemCode('')
      setRedeemMsg({ ok: true, text: '兑换成功，金额已到账' })
      toast('兑换成功')
      await load()
    } catch (err) {
      setRedeemMsg({ ok: false, text: err instanceof ApiError ? err.message : '兑换失败' })
    } finally {
      setRedeeming(false)
    }
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-lg font-semibold text-slate-900">
          账单充值 <span className="text-sm font-normal text-slate-400">Billing</span>
        </h1>
        <p className="mt-0.5 text-sm text-slate-500">账户余额、充值方式与交易记录。</p>
      </header>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">{error}</div>
      )}

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
        {/* 左栏 */}
        <div className="space-y-5 lg:col-span-2">
          <Card>
            <CardContent className="flex flex-wrap items-center justify-between gap-4 p-6">
              <div>
                <div className="text-sm text-slate-500">余额概览 Balance Overview</div>
                <div className="mt-1 flex items-baseline gap-1">
                  <span className="text-3xl font-extrabold tabular-nums text-slate-900">
                    ${((user?.balance_cents ?? 0) / 100).toFixed(2)}
                  </span>
                  <span className="text-sm text-slate-400">USD</span>
                </div>
                <p className="mt-1 text-xs text-slate-400">
                  托管余额 ${((user?.hosting_available_cents ?? 0) / 100).toFixed(2)} 可用
                </p>
              </div>
              <div className="flex items-center gap-2 rounded-xl border-2 border-black bg-sage/70 px-3 py-2 text-xs font-bold text-black">
                <ShieldCheckIcon className="h-4 w-4" />
                账户安全验证已开启
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader title={<span>支付方式 <span className="text-xs font-normal text-slate-400">Payment Method</span></span>} />
            <CardContent className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {PAY_METHODS.map((m) => {
                const Icon = m.icon
                const active = method === m.id
                return (
                  <button
                    key={m.id}
                    onClick={() => setMethod(m.id)}
                    className={cn(
                      'flex items-center gap-3 rounded-xl border-2 p-4 text-left transition-all duration-100',
                      active
                        ? 'border-black bg-mint shadow-hard-sm'
                        : 'border-black/25 bg-white hover:border-black hover:shadow-hard-sm',
                    )}
                  >
                    <div
                      className={cn(
                        'flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border-2 border-black',
                        active ? 'bg-white' : 'bg-slate-100',
                      )}
                    >
                      <Icon className="h-5 w-5" />
                    </div>
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-slate-900">
                        {m.cn} <span className="text-xs font-normal text-slate-400">{m.name}</span>
                      </div>
                      <div className="text-xs text-slate-400">{m.desc}</div>
                    </div>
                  </button>
                )
              })}
            </CardContent>
          </Card>

          <Card>
            <CardHeader title={<span>充值金额 <span className="text-xs font-normal text-slate-400">Amount</span></span>} />
            <CardContent>
              <div className="flex flex-wrap gap-2.5">
                {QUICK_AMOUNTS.map((v, i) => {
                  const tones = ['bg-mint', 'bg-lavender', 'bg-lemon', 'bg-peach', 'bg-sage', 'bg-sky-200']
                  return (
                  <button
                    key={v}
                    onClick={() => setAmount(String(v))}
                    className={cn(
                      'rounded-xl border-2 px-5 py-2.5 text-sm font-bold tabular-nums transition-all duration-100',
                      Number(amount) === v
                        ? cn('border-black text-black shadow-hard-sm', tones[i % tones.length])
                        : 'border-black/25 bg-white text-slate-600 hover:border-black hover:shadow-hard-sm',
                    )}
                  >
                    ${v}
                  </button>
                  )
                })}
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-sm text-slate-400">$</span>
                  <Input
                    className="h-[42px] w-32 pl-7 font-semibold tabular-nums"
                    inputMode="decimal"
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                  />
                </div>
              </div>

              <div className="mt-4 rounded-lg bg-slate-50 p-4 text-xs leading-5 text-slate-500">
                <p>到账时间：支付宝 / 微信 / 信用卡通常 1-3 分钟内到账；加密货币 (TRC20) 需 1 个区块确认。</p>
                <p className="mt-1">最低充值金额：$1 USD。充值后余额可用于购买实例与续费。</p>
              </div>

              <Button
                className="mt-4 h-11 w-full text-base sm:w-auto sm:px-10"
                disabled={!validAmount || submitting}
                onClick={submit}
              >
                {submitting ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '立即充值'}
              </Button>
            </CardContent>
          </Card>

          {/* 兑换码充值 */}
          <Card>
            <CardHeader title={<span>兑换码 <span className="text-xs font-normal text-slate-400">Gift Card</span></span>} subtitle="使用管理员发放的礼品卡/兑换码充值余额" />
            <CardContent>
              <div className="flex gap-2">
                <Input
                  className="font-mono"
                  value={redeemCode}
                  onChange={(e) => setRedeemCode(e.target.value)}
                  placeholder="如 GIFT-XXXX-XXXX"
                  onKeyDown={(e) => e.key === 'Enter' && redeem()}
                />
                <Button className="shrink-0" onClick={redeem} disabled={redeeming || !redeemCode.trim()}>
                  {redeeming ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '兑换'}
                </Button>
              </div>
              {redeemMsg && (
                <p className={cn('mt-2 text-sm', redeemMsg.ok ? 'text-emerald-600' : 'text-red-600')}>{redeemMsg.text}</p>
              )}
            </CardContent>
          </Card>
        </div>

        {/* 右栏：交易记录 */}
        <Card className="h-fit">
          <CardHeader title={<span>交易记录 <span className="text-xs font-normal text-slate-400">Transactions</span></span>} />
          <CardContent className="space-y-3">
            <div className="relative">
              <SearchIcon className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <Input className="pl-8" placeholder="搜索记录..." value={query} onChange={(e) => setQuery(e.target.value)} />
            </div>

            <div className="flex flex-wrap gap-1.5">
              {TYPE_FILTERS.map((f) => (
                <button
                  key={f.key}
                  onClick={() => setTypeFilter(f.key)}
                  className={cn(
                    'rounded-full border-2 px-2.5 py-1 text-xs font-bold transition-all duration-100',
                    typeFilter === f.key
                      ? 'border-black bg-lavender text-black shadow-hard-sm'
                      : 'border-black/25 bg-white text-slate-500 hover:border-black hover:text-slate-700',
                  )}
                >
                  {f.label}
                </button>
              ))}
            </div>

            <div className="divide-y divide-slate-100">
              {filtered.length === 0 ? (
                <p className="py-10 text-center text-sm text-slate-400">暂无匹配的交易记录</p>
              ) : (
                filtered.map((t) => {
                  // 管理员调整：方向由描述里的 +/- 表达，金额恒为正
                  const isAdjust = t.type === 'admin_adjust'
                  const positive = POSITIVE_TYPES.has(t.type)
                  return (
                    <div key={t.id} className="py-3">
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <p className="truncate text-sm font-medium text-slate-700">{t.description}</p>
                          <p className="mt-0.5 text-xs text-slate-400">{fmtDate(t.created_at)}</p>
                        </div>
                        {!isAdjust && (
                          <span className={cn('shrink-0 text-sm font-semibold tabular-nums', positive ? 'text-emerald-600' : 'text-red-600')}>
                            {positive ? '+' : '-'}${(t.amount_cents / 100).toFixed(2)}
                          </span>
                        )}
                      </div>
                      <div className="mt-1.5 flex items-center gap-2">
                        <Badge color={isAdjust ? 'purple' : positive ? 'green' : 'red'}>{TYPE_LABEL[t.type] ?? t.type}</Badge>
                        <Badge color={t.status === 'success' ? 'green' : 'orange'}>{t.status === 'success' ? '成功' : '处理中'}</Badge>
                      </div>
                    </div>
                  )
                })
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
