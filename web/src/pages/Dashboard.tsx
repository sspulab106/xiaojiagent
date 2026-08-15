import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import type { Announcement, Instance, User } from '../lib/types'
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  Progress,
  StatCard,
  Switch,
  Tabs,
  cn,
  fmtBytes,
  fmtDate,
  pastelTone,
  useToast,
} from '../components/ui'
import {
  BellIcon,
  CalendarIcon,
  CpuIcon,
  GlobeIcon,
  HardDriveIcon,
  MemoryIcon,
  PlusIcon,
  WalletIcon,
} from '../components/icons'

function statusBadge(status: string) {
  if (status === 'running') return <Badge color="green" dot>运行中</Badge>
  if (status === 'stopped') return <Badge color="slate" dot>已停止</Badge>
  return <Badge color="orange" dot>{status}</Badge>
}

export default function Dashboard() {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [user, setUser] = useState<User | null>(null)
  const [instances, setInstances] = useState<Instance[]>([])
  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [tab, setTab] = useState('all')
  const [renewBusy, setRenewBusy] = useState<number | null>(null)
  const toast = useToast()
  const navigate = useNavigate()

  const load = useCallback(async () => {
    try {
      const [profile, inst, anns] = await Promise.all([api.profile(), api.instances(), api.announcements()])
      setUser(profile)
      setInstances(inst)
      setAnnouncements(anns)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const toggleAutoRenew = async (inst: Instance, enabled: boolean) => {
    setRenewBusy(inst.id)
    try {
      await api.setAutoRenew(inst.id, enabled)
      setInstances((prev) => prev.map((i) => (i.id === inst.id ? { ...i, auto_renew: enabled } : i)))
      toast(enabled ? '已开启自动续费' : '已关闭自动续费')
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '更新自动续费失败')
    } finally {
      setRenewBusy(null)
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
    return (
      <Card className="p-8 text-center text-sm text-red-600">{error}</Card>
    )
  }

  const runningCount = instances.filter((i) => i.status === 'running').length
  const filtered = tab === 'all' ? instances : instances.filter((i) => i.status === tab)

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-slate-900">
            控制面板 <span className="text-sm font-normal text-slate-400">Dashboard</span>
          </h1>
          <p className="mt-0.5 text-sm text-slate-500">欢迎回来，{user?.username}</p>
        </div>
        <button
          onClick={() => navigate('/instances?create=1')}
          className="inline-flex h-10 items-center justify-center gap-1.5 rounded-xl border-2 border-black bg-mint px-4 text-sm font-bold text-black shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none"
        >
          <PlusIcon className="h-4 w-4" />
          新建实例
        </button>
      </header>

      {/* 顶部统计 */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard tone="bg-mint/50" label="可用余额" en="Available Balance" value={`$${((user?.balance_cents ?? 0) / 100).toFixed(2)}`} icon={<WalletIcon />}>
          <button
            onClick={() => navigate('/recharge')}
            className="inline-flex items-center gap-0.5 text-sm font-medium text-blue-600 transition-colors hover:text-blue-700 hover:underline"
          >
            充值 Recharge
          </button>
        </StatCard>

        <StatCard tone="bg-lavender/45" label="托管余额" en="Hosting Balance" value={`$${((user?.hosting_total_cents ?? 0) / 100).toFixed(2)}`}>
          <div className="space-y-1 text-xs text-slate-400">
            <div className="flex items-center justify-between">
              <span>可用 Available</span>
              <span className="font-semibold tabular-nums text-emerald-600">
                ${((user?.hosting_available_cents ?? 0) / 100).toFixed(2)}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span>冻结 Frozen</span>
              <span className="font-semibold tabular-nums text-amber-600">
                ${((user?.hosting_frozen_cents ?? 0) / 100).toFixed(2)}
              </span>
            </div>
          </div>
        </StatCard>

        <StatCard tone="bg-sky-200/70" label="我的实例" en="Total Instances" value={instances.length}>
          <span className="text-sm text-slate-500">
            运行中 <b className="font-semibold text-emerald-600">{runningCount}</b> 台
          </span>
        </StatCard>

        <StatCard tone="bg-lemon/50" label="平台公告" en="Announcements" value={announcements.length > 0 ? announcements[0].title : '暂无公告'} icon={<BellIcon />}>
          {announcements.length > 0 && (
            <div className="space-y-1">
              {announcements.slice(0, 2).map((a) => (
                <div key={a.id} className="flex items-center gap-1.5 text-xs text-slate-400">
                  <span className="h-1 w-1 rounded-full bg-blue-500" />
                  <span className="truncate">{a.title}</span>
                  <span className="ml-auto shrink-0 font-mono">{fmtDate(a.created_at)}</span>
                </div>
              ))}
            </div>
          )}
        </StatCard>
      </div>

      {/* 我的实例 */}
      <Card>
        <CardHeader
          title={
            <span>
              我的实例 <span className="text-xs font-normal text-slate-400">My Instances</span>
            </span>
          }
          right={
            <Tabs
              value={tab}
              onChange={setTab}
              options={[
                { key: 'all', label: '全部' },
                { key: 'running', label: '运行中' },
                { key: 'stopped', label: '已停止' },
              ]}
            />
          }
        />
        <CardContent>
          {filtered.length === 0 ? (
            <div className="py-16 text-center">
              <p className="text-sm text-slate-400">该分类下暂无实例</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
              {filtered.map((inst, idx) => (
                <InstanceCard
                  key={inst.id}
                  instance={inst}
                  renewBusy={renewBusy === inst.id}
                  tone={pastelTone(idx)}
                  onOpen={() => navigate(`/instances/${inst.id}`)}
                  onToggleRenew={(v) => toggleAutoRenew(inst, v)}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function InstanceCard({
  instance,
  renewBusy,
  tone,
  onOpen,
  onToggleRenew,
}: {
  instance: Instance
  renewBusy: boolean
  tone: string
  onOpen: () => void
  onToggleRenew: (v: boolean) => void
}) {
  const country = instance.country?.trim() || '?'
  const used = instance.traffic_used_up_bytes + instance.traffic_used_down_bytes
  const total = instance.traffic_gb * 1024 ** 3
  const unlimited = instance.traffic_gb === 0

  return (
    <div
      onClick={onOpen}
      className="group cursor-pointer rounded-2xl border-2 border-black bg-white p-5 shadow-hard transition-all duration-100 hover:-translate-y-1 hover:shadow-hard-lg active:translate-y-0.5 active:shadow-none"
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <span className={cn('flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border-2 border-black text-sm font-extrabold text-black', tone)}>
            {country.charAt(0).toUpperCase()}
          </span>
          <span className="rounded-md border border-black/20 bg-white px-1.5 py-0.5 text-xs font-bold text-slate-600">
            {country}
          </span>
        </div>
        {statusBadge(instance.status)}
      </div>

      <h3 className="mt-3 truncate text-sm font-extrabold text-slate-900 transition-colors group-hover:text-brand">
        {instance.display_name || instance.name}
      </h3>

      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs font-medium text-slate-500">
        <span className="inline-flex items-center gap-1">
          <CpuIcon className="h-3.5 w-3.5" />
          {instance.cpu_cores} vCPU
        </span>
        <span className="inline-flex items-center gap-1">
          <MemoryIcon className="h-3.5 w-3.5" />
          {instance.memory_mb}M
        </span>
        <span className="inline-flex items-center gap-1">
          <HardDriveIcon className="h-3.5 w-3.5" />
          {fmtBytes(instance.disk_mb * 1024 * 1024)}
        </span>
      </div>

      <div className="mt-3 flex items-center gap-2">
        <span className="rounded-md border border-black/20 bg-sky-200 px-2 py-0.5 text-xs font-bold text-black">
          {instance.os || instance.image}
        </span>
        <span className="truncate text-xs font-medium text-slate-400">{instance.node_name}</span>
      </div>

      <div className="mt-4">
        <div className="mb-1 flex items-center justify-between text-xs">
          <span className="text-slate-400">流量 Traffic</span>
          <span className="font-medium tabular-nums text-slate-600">
            {unlimited ? '无限' : `${fmtBytes(used)} / ${instance.traffic_gb} GB`}
          </span>
        </div>
        {!unlimited && <Progress value={used} max={total || 1} />}
      </div>

      <div className="mt-3 flex items-center gap-1.5 text-xs text-slate-500">
        <GlobeIcon className="h-3.5 w-3.5" />
        <span className="font-mono">{instance.ip}</span>
      </div>

      <div className="mt-4 flex items-center justify-between border-t border-slate-100 pt-3">
        <div>
          <div className="font-mono text-sm font-semibold tabular-nums text-brand">
            ${(instance.price_cents / 100).toFixed(2)}/月
          </div>
          <div className="mt-0.5 flex items-center gap-1 text-xs text-slate-400">
            <CalendarIcon className="h-3 w-3" />
            {instance.expires_at ? fmtDate(instance.expires_at) : '未设置'}
          </div>
        </div>
        <div
          className="flex items-center gap-1.5"
          onClick={(e) => {
            e.stopPropagation()
          }}
        >
          <span className="text-xs text-slate-400">自动续费</span>
          <Switch checked={instance.auto_renew} disabled={renewBusy} onChange={onToggleRenew} />
        </div>
      </div>
    </div>
  )
}
