import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { parseSelfCheckMeta, useNodeSelfCheck } from '../lib/useNodeSelfCheck'
import { detectIpInfo } from '../lib/geo'
import type { Coupon, HostResult, ImagePreset, Instance, Node, NodeDetail, NodeStats, Package } from '../lib/types'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  ConfirmDialog,
  CopyButton,
  Empty,
  Field,
  Input,
  Modal,
  Select,
  Switch,
  cn,
  fmtBytes,
  fmtDate,
  useToast,
} from '../components/ui'
import { HistorySparkline, LiveBar, useHistory } from '../components/charts'
import FirewallManager from '../components/FirewallManager'
import {
  CreditCardIcon,
  EyeIcon,
  EyeOffIcon,
  GlobeIcon,
  InfoIcon,
  PlusIcon,
  ShieldCheckIcon,
  ShieldIcon,
  TagIcon,
} from '../components/icons'

function fmtUptime(sec: number): string {
  if (!sec || sec <= 0) return '-'
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  return `${days}d ${hours}h`
}

function num(v: unknown): number {
  return typeof v === 'number' ? v : 0
}

// datetime-local input <-> ISO string
function toLocalInput(s: string | null | undefined): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

function fromLocalInput(v: string): string | null {
  if (!v) return null
  const d = new Date(v)
  return isNaN(d.getTime()) ? null : d.toISOString()
}

const RULES = [
  { icon: <GlobeIcon className="h-5 w-5" />, title: '全球节点', desc: '独立 IP 托管，就近接入' },
  { icon: <ShieldCheckIcon className="h-5 w-5" />, title: '安全稳定', desc: '实时监控，异常自动切换' },
  { icon: <TagIcon className="h-5 w-5" />, title: '按量计费', desc: '透明价格，收益入托管余额' },
  { icon: <CreditCardIcon className="h-5 w-5" />, title: '灵活上架', desc: '自建套餐，一键上架出售' },
]

export default function Hosting() {
  const toast = useToast()
  const [machines, setMachines] = useState<Node[]>([])
  const [instances, setInstances] = useState<Instance[]>([])
  const [images, setImages] = useState<ImagePreset[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [fwOpen, setFwOpen] = useState(false)
  const [detail, setDetail] = useState<NodeDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [error, setError] = useState('')
  const [showToken, setShowToken] = useState(false)

  // 所选节点的套餐 / 优惠码
  const [nodePackages, setNodePackages] = useState<Package[]>([])
  const [nodeCoupons, setNodeCoupons] = useState<Coupon[]>([])

  // 实时监控
  const [nodeStats, setNodeStats] = useState<NodeStats | null>(null)
  const [statsLoading, setStatsLoading] = useState(false)
  const { push } = useHistory(20)
  const [cpuHist, setCpuHist] = useState<number[]>([])
  const [memHist, setMemHist] = useState<number[]>([])
  const [netInHist, setNetInHist] = useState<number[]>([])
  const [netOutHist, setNetOutHist] = useState<number[]>([])

  // 添加机器
  const [hostOpen, setHostOpen] = useState(false)
  const [hostLoading, setHostLoading] = useState(false)
  const [hostError, setHostError] = useState('')
  const [hostResult, setHostResult] = useState<HostResult | null>(null)
  const [hostName, setHostName] = useState('')
  const [hostAddr, setHostAddr] = useState('')
  const [hostRegion, setHostRegion] = useState('')
  const [hostVirtType, setHostVirtType] = useState('incus')
  const [hostWithToken, setHostWithToken] = useState(true)
  const [hostWebPassword, setHostWebPassword] = useState('')
  const [autoDetecting, setAutoDetecting] = useState(false)

  // 添加套餐（editingPkg 非空时 = 编辑模式）
  const [pkgOpen, setPkgOpen] = useState(false)
  const [pkgSaving, setPkgSaving] = useState(false)
  const [pkgError, setPkgError] = useState('')
  const [editingPkg, setEditingPkg] = useState<Package | null>(null)
  const [pkgName, setPkgName] = useState('')
  const [pkgImgRefs, setPkgImgRefs] = useState<string[]>([])
  const [pkgCpu, setPkgCpu] = useState('')
  const [pkgMem, setPkgMem] = useState('')
  const [pkgDisk, setPkgDisk] = useState('')
  const [pkgTraffic, setPkgTraffic] = useState('')
  const [pkgSlots, setPkgSlots] = useState('')
  const [pkgIpv6, setPkgIpv6] = useState(false)
  const [pkgPrice, setPkgPrice] = useState('')
  const [pkgListed, setPkgListed] = useState(false)
  const [pkgEarlyRefund, setPkgEarlyRefund] = useState(false)

  // 添加/编辑优惠码（editingCp 非空时 = 编辑模式）
  const [cpOpen, setCpOpen] = useState(false)
  const [cpSaving, setCpSaving] = useState(false)
  const [cpError, setCpError] = useState('')
  const [editingCp, setEditingCp] = useState<Coupon | null>(null)
  const [cpCode, setCpCode] = useState('')
  const [cpPercent, setCpPercent] = useState('')
  const [cpMaxUses, setCpMaxUses] = useState('')
  const [cpEnabled, setCpEnabled] = useState(true)
  const [cpExpires, setCpExpires] = useState('')

  // 删除确认
  const [confirm, setConfirm] = useState<{ title: string; message: string; action: () => Promise<boolean> } | null>(null)

  // 节点自检（远程运行 verify-ndp.sh，轮询输出）—— 托管中心租户端点
  const sc = useNodeSelfCheck(false)

  const loadAll = useCallback(async () => {
    try {
      const [list, insts, imgs] = await Promise.all([api.nodes(), api.instances(), api.images()])
      setMachines(list)
      setInstances(insts)
      setImages(imgs)
      setError('')
      setSelectedId((prev) => prev ?? list[0]?.id ?? null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载数据失败')
    }
  }, [])

  useEffect(() => {
    loadAll()
  }, [loadAll])

  useEffect(() => {
    if (selectedId === null) {
      setDetail(null)
      setNodePackages([])
      setNodeCoupons([])
      setNodeStats(null)
      return
    }
    let cancelled = false
    setDetailLoading(true)
    api
      .nodeDetail(selectedId)
      .then((d) => {
        if (!cancelled) {
          setDetail(d)
          setShowToken(false)
        }
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setDetailLoading(false)
      })
    api.nodePackages(selectedId).then(setNodePackages).catch(() => {})
    api.nodeCoupons(selectedId).then(setNodeCoupons).catch(() => {})
    return () => {
      cancelled = true
    }
  }, [selectedId])

  // 节点实时监控轮询（15s，降低采集频率与宿主机负载）
  useEffect(() => {
    if (selectedId === null) return
    let stop = false
    const poll = async () => {
      if (stop) return
      try {
        const s = await api.nodeStats(selectedId)
        if (stop) return
        setNodeStats(s)
        setCpuHist((h) => push(h, s.host.cpu_percent))
        setMemHist((h) => push(h, s.host.mem_used_mb))
        setNetInHist((h) => push(h, s.host.net_in_bps))
        setNetOutHist((h) => push(h, s.host.net_out_bps))
        setStatsLoading(false)
      } catch {
        if (!stop) setStatsLoading(false)
      }
    }
    setStatsLoading(true)
    poll()
    const t = setInterval(poll, 15000)
    return () => {
      stop = true
      clearInterval(t)
    }
  }, [selectedId, push])

  const openHost = () => {
    setHostError('')
    setHostResult(null)
    setHostName('')
    setHostAddr('')
    setHostRegion('')
    setHostVirtType('incus')
    setHostWithToken(true)
    setHostWebPassword('')
    setHostOpen(true)
  }

  const submitHost = async () => {
    setHostLoading(true)
    setHostError('')
    try {
      const res = await api.addMachine({
        name: hostName,
        agent_addr: hostAddr || undefined,
        region: hostRegion || undefined,
        virt_type: hostVirtType,
        with_token: hostWithToken,
        web_password: hostWebPassword || undefined,
      })
      setHostResult(res)
      toast('机器安装包已生成')
      await loadAll()
    } catch (err) {
      setHostError(err instanceof ApiError ? err.message : '添加机器失败')
    } finally {
      setHostLoading(false)
    }
  }

  const runDetect = async () => {
    setAutoDetecting(true)
    setHostError('')
    try {
      const info = await detectIpInfo()
      if (info.ipv4) {
        setHostAddr(`http://${info.ipv4}:8792`)
      } else if (info.ipv6) {
        setHostAddr(`http://[${info.ipv6}]:8792`)
      }
      if (info.region) {
        setHostRegion(info.region)
        toast(`已识别地区：${info.region}`)
      }
      if (!info.ipv4 && !info.ipv6) {
        toast('未获取到公网 IP，请手动填写地址')
      } else {
        toast('已获取本机公网地址，可修改后使用')
      }
    } catch {
      toast('自动检测失败，请手动填写')
    } finally {
      setAutoDetecting(false)
    }
  }

  const togglePkgImage = (ref: string) => {
    setPkgImgRefs((prev) => (prev.includes(ref) ? prev.filter((r) => r !== ref) : [...prev, ref]))
  }

  const openAddPackage = () => {
    setEditingPkg(null)
    setPkgError('')
    setPkgName('')
    setPkgImgRefs([])
    setPkgCpu('1')
    setPkgMem('512')
    setPkgDisk('5120')
    setPkgTraffic('500')
    setPkgSlots('5')
    setPkgIpv6(false)
    setPkgPrice('199')
    setPkgListed(false)
    setPkgEarlyRefund(false)
    setPkgOpen(true)
  }

  const openEditPackage = (p: Package) => {
    setEditingPkg(p)
    setPkgError('')
    setPkgName(p.name)
    setPkgImgRefs(p.images ? p.images.split(',').filter(Boolean) : [])
    setPkgCpu(String(p.cpu_cores))
    setPkgMem(String(p.memory_mb))
    setPkgDisk(String(p.disk_mb))
    setPkgTraffic(String(p.traffic_gb))
    setPkgSlots(String(p.port_slots))
    setPkgIpv6(p.ipv6)
    setPkgPrice(String(p.price_cents))
    setPkgListed(p.listed)
    setPkgEarlyRefund(p.early_full_refund)
    setPkgOpen(true)
  }

  const submitPackage = async () => {
    if (!selectedId) return
    setPkgSaving(true)
    setPkgError('')
    try {
      const body = {
        name: pkgName,
        images: pkgImgRefs,
        cpu_cores: Number(pkgCpu) || 0,
        memory_mb: Number(pkgMem) || 0,
        disk_mb: Number(pkgDisk) || 0,
        traffic_gb: Number(pkgTraffic) || 0,
        port_slots: Number(pkgSlots) || 0,
        ipv6: pkgIpv6,
        price_cents: Number(pkgPrice) || 0,
        listed: pkgListed,
        early_full_refund: pkgEarlyRefund,
      }
      if (editingPkg) {
        await api.updatePackage(editingPkg.id, { ...body, images: pkgImgRefs.join(',') })
        toast('套餐已更新')
      } else {
        await api.addNodePackage(selectedId, body)
        toast('套餐已添加')
      }
      setPkgOpen(false)
      if (selectedId) setNodePackages(await api.nodePackages(selectedId))
    } catch (err) {
      setPkgError(err instanceof ApiError ? err.message : '保存套餐失败')
    } finally {
      setPkgSaving(false)
    }
  }

  const toggleListed = async (p: Package) => {
    try {
      await api.updatePackage(p.id, { listed: !p.listed })
      if (selectedId) setNodePackages(await api.nodePackages(selectedId))
      toast(p.listed ? '已下架' : '已上架')
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '操作失败')
    }
  }

  const removePkg = (p: Package) => {
    setConfirm({
      title: '删除套餐',
      message: `确定删除套餐「${p.name}」吗？删除后不可恢复。`,
      action: async () => {
        try {
          await api.deletePackage(p.id)
          toast('套餐已删除')
          if (selectedId) setNodePackages(await api.nodePackages(selectedId))
          return true
        } catch (err) {
          toast(err instanceof ApiError ? err.message : '删除失败')
          return false
        }
      },
    })
  }

  const openAddCoupon = () => {
    setEditingCp(null)
    setCpError('')
    setCpCode('')
    setCpPercent('10')
    setCpMaxUses('0')
    setCpEnabled(true)
    setCpExpires('')
    setCpOpen(true)
  }

  const openEditCoupon = (c: Coupon) => {
    setEditingCp(c)
    setCpError('')
    setCpCode(c.code)
    setCpPercent(String(c.percent_off))
    setCpMaxUses(String(c.max_uses))
    setCpEnabled(c.enabled)
    setCpExpires(toLocalInput(c.expires_at))
    setCpOpen(true)
  }

  const submitCoupon = async () => {
    if (!selectedId) return
    setCpSaving(true)
    setCpError('')
    try {
      if (editingCp) {
        await api.updateNodeCoupon(editingCp.id, {
          percent_off: cpPercent.trim() !== '' ? Number(cpPercent) : undefined,
          max_uses: cpMaxUses.trim() !== '' ? Number(cpMaxUses) : undefined,
          enabled: cpEnabled,
          expires_at: fromLocalInput(cpExpires) ?? undefined,
          clear_expiry: cpExpires.trim() === '' && !!editingCp.expires_at,
        })
        toast('优惠码已更新')
      } else {
        await api.addNodeCoupon(selectedId, {
          code: cpCode,
          percent_off: Number(cpPercent) || 0,
          max_uses: Number(cpMaxUses) || 0,
          enabled: cpEnabled,
          expires_at: fromLocalInput(cpExpires) ?? undefined,
        })
        toast('优惠码已创建')
      }
      setCpOpen(false)
      if (selectedId) setNodeCoupons(await api.nodeCoupons(selectedId))
    } catch (err) {
      setCpError(err instanceof ApiError ? err.message : '保存优惠码失败')
    } finally {
      setCpSaving(false)
    }
  }

  const removeCp = (c: Coupon) => {
    setConfirm({
      title: '删除优惠码',
      message: `确定删除优惠码「${c.code}」吗？`,
      action: async () => {
        try {
          await api.deleteNodeCoupon(c.id)
          toast('优惠码已删除')
          if (selectedId) setNodeCoupons(await api.nodeCoupons(selectedId))
          return true
        } catch (err) {
          toast(err instanceof ApiError ? err.message : '删除失败')
          return false
        }
      },
    })
  }

  if (error) {
    return <Card className="p-8 text-center text-sm text-red-600">{error}</Card>
  }

  const selected = detail?.node ?? machines.find((m) => m.id === selectedId) ?? null
  const nodeInstances = selected ? instances.filter((i) => i.node_name === selected.name) : []
  const hs = nodeStats?.host
  const memPct = hs && hs.mem_total_mb > 0 ? (hs.mem_used_mb / hs.mem_total_mb) * 100 : 0
  const diskPct = hs && hs.disk_total_mb > 0 ? (hs.disk_used_mb / hs.disk_total_mb) * 100 : 0

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-lg font-semibold text-slate-900">
          托管中心 <span className="text-sm font-normal text-slate-400">Hosting Center</span>
        </h1>
        <p className="mt-0.5 text-sm text-slate-500">托管你的宿主机，创建套餐上架出售，实时监控资源使用。</p>
      </header>

      {/* 规则横幅 */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {RULES.map((r) => (
          <Card key={r.title}>
            <CardContent className="flex items-start gap-3 p-5">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600">
                {r.icon}
              </div>
              <div>
                <div className="text-sm font-semibold text-slate-900">{r.title}</div>
                <div className="mt-0.5 text-xs text-slate-400">{r.desc}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="flex flex-col gap-6 lg:flex-row">
        {/* 机器列表 */}
        <div className="w-full shrink-0 lg:w-72">
          <Card>
            <CardHeader
              title={
                <span>
                  我的机器 <span className="text-xs font-normal text-slate-400">Servers</span>
                </span>
              }
              subtitle={`共 ${machines.length} 台`}
              right={
                <Button size="sm" variant="outline" onClick={openHost}>
                  <PlusIcon className="h-3.5 w-3.5" />
                  添加机器
                </Button>
              }
            />
            <CardContent>
              {machines.length === 0 ? (
                <Empty text="暂无机器，点击「添加机器」接入" />
              ) : (
                <div className="space-y-1">
                  {machines.map((m) => (
                    <button
                      key={m.id}
                      onClick={() => setSelectedId(m.id)}
                      className={cn(
                        'w-full rounded-lg px-3 py-2.5 text-left transition-colors',
                        selectedId === m.id ? 'bg-blue-50' : 'hover:bg-slate-50',
                      )}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className={cn('text-sm font-medium', selectedId === m.id ? 'text-blue-700' : 'text-slate-800')}>
                          {m.name}
                        </span>
                        <Badge color={m.status === 'online' ? 'green' : 'red'} dot>
                          {m.status === 'online' ? '在线' : '离线'}
                        </Badge>
                      </div>
                      <div className="mt-0.5 flex items-center justify-between text-xs text-slate-400">
                        <span>{m.country || m.region || '未知地区'}</span>
                        <span>{m.instance_count ?? 0} 个实例</span>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* 详情 */}
        <div className="min-w-0 flex-1">
          {detailLoading ? (
            <div className="flex h-64 items-center justify-center">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-blue-600" />
            </div>
          ) : !selected ? (
            <Card className="p-8">
              <Empty text="请选择或添加一台宿主机" icon={<InfoIcon className="h-8 w-8" />} />
            </Card>
          ) : (
            <div className="space-y-6">
              {/* 基本信息 */}
              <Card>
                <CardHeader
                  title={
                    <span>
                      {selected.name}{' '}
                      <span className="text-xs font-normal text-slate-400">
                        {selected.tags?.split(',').filter(Boolean).join(' · ')}
                      </span>
                    </span>
                  }
                  subtitle={selected.notes || '暂无备注'}
                  right={
                    <div className="flex items-center gap-2">
                      <Badge color={selected.status === 'online' ? 'green' : 'red'} dot>
                        {selected.status === 'online' ? '在线' : '离线'}
                      </Badge>
                      <Button
                        size="sm"
                        variant="sage"
                        disabled={selected.status !== 'online' || sc.starting}
                        onClick={() =>
                          sc.start(selected, (st) => {
                            // 自检完成后局部刷新「最近自检」徽章（含左侧机器列表与详情）
                            const snap = JSON.stringify({
                              status: st.status,
                              run_id: st.id,
                              exit_code: st.exit_code,
                              started_at: st.started_at,
                              finished_at: st.finished_at,
                              output: st.output,
                            })
                            setMachines((prev) =>
                              prev.map((m) => (m.id === selected.id ? { ...m, last_selfcheck: snap } : m)),
                            )
                            setDetail((prev) =>
                              prev && prev.node.id === selected.id
                                ? { ...prev, node: { ...prev.node, last_selfcheck: snap } }
                                : prev,
                            )
                          })
                        }
                        title={selected.status !== 'online' ? '节点离线，无法远程自检' : '在节点上运行 verify-ndp.sh 并回显结果'}
                      >
                        自检
                      </Button>
                    </div>
                  }
                />
                <CardContent className="space-y-5">
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                    <div>
                      <div className="text-xs text-slate-400">IPv4</div>
                      <div className="mt-0.5 font-mono text-sm text-slate-800">{selected.host_ip || '-'}</div>
                    </div>
                    <div>
                      <div className="text-xs text-slate-400">IPv6</div>
                      <div className="mt-0.5 font-mono text-sm text-slate-800">{selected.ipv6 || '-'}</div>
                    </div>
                    <div>
                      <div className="text-xs text-slate-400">地区</div>
                      <div className="mt-0.5 text-sm text-slate-800">{selected.country || selected.region || '-'}</div>
                    </div>
                  </div>

                  {/* 最近自检结果 */}
                  {(() => {
                    const scm = parseSelfCheckMeta(selected)
                    if (!scm || !scm.status) return null
                    const running = scm.status === 'running'
                    const stale =
                      running && !!scm.started_at && Date.now() - new Date(scm.started_at).getTime() > 16 * 60 * 1000
                    const ok = scm.status === 'done'
                    const label = running ? (stale ? '中断' : '运行中') : ok ? '通过' : '失败'
                    const color = running ? (stale ? 'slate' : 'blue') : ok ? 'green' : 'red'
                    return (
                      <div className="flex flex-wrap items-center gap-2 text-xs">
                        <span className="text-slate-400">最近自检</span>
                        <Badge color={color} dot>{label}</Badge>
                        {scm.finished_at && <span className="font-mono text-slate-400">{fmtDate(scm.finished_at)}</span>}
                        {!running && scm.exit_code !== undefined && (
                          <span className="font-mono text-slate-400">退出码 {scm.exit_code}</span>
                        )}
                        {stale && <span className="text-slate-400">（已超时，可重新触发）</span>}
                      </div>
                    )
                  })()}

                  {/* 实时资源 */}
                  {hs ? (
                    <>
                      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                        <HistorySparkline label="CPU 使用率" value={`${hs.cpu_percent.toFixed(1)}%`} data={cpuHist} max={100} color="#0066FF" />
                        <HistorySparkline label="内存" value={`${fmtBytes(hs.mem_used_mb * 1024 * 1024)} / ${fmtBytes(hs.mem_total_mb * 1024 * 1024)}`} data={memHist} color="#16A34A" />
                        <HistorySparkline label="下行速率" value={`${fmtBytes(hs.net_in_bps)}/s`} data={netInHist} color="#0A75FF" />
                        <HistorySparkline label="上行速率" value={`${fmtBytes(hs.net_out_bps)}/s`} data={netOutHist} color="#F59E0B" />
                      </div>
                      <div className="space-y-3">
                        <div>
                          <div className="mb-1 flex justify-between font-mono text-xs">
                            <span className="text-slate-400">内存</span>
                            <span>{hs.mem_used_mb.toFixed(0)} MB / {hs.mem_total_mb} MB</span>
                          </div>
                          <LiveBar value={hs.mem_used_mb} max={hs.mem_total_mb} />
                        </div>
                        <div>
                          <div className="mb-1 flex justify-between font-mono text-xs">
                            <span className="text-slate-400">磁盘</span>
                            <span>{fmtBytes(hs.disk_used_mb * 1024 * 1024)} / {fmtBytes(hs.disk_total_mb * 1024 * 1024)}</span>
                          </div>
                          <LiveBar value={hs.disk_used_mb} max={hs.disk_total_mb} color="bg-amber-500" />
                        </div>
                        <div className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-slate-400">
                          <span>CORES {hs.total_cores}</span>
                          <span>LOAD {hs.load1.toFixed(2)}/{hs.load5.toFixed(2)}/{hs.load15.toFixed(2)}</span>
                          <span>VMS {hs.running_vms}/{hs.total_vms}</span>
                          <span>UPTIME {fmtUptime(hs.uptime)}</span>
                        </div>
                        {hs.ipv6_mode && hs.ipv6_mode !== 'none' && (
                          <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-slate-100 bg-slate-50/60 px-2.5 py-2 font-mono text-[11px]">
                            <span className="font-sans text-slate-400">IPv6 模式</span>
                            <span className={`rounded-full px-1.5 py-0.5 font-semibold ${hs.ipv6_mode === 'subnet' ? 'bg-lavender-100 text-lavender-700' : 'bg-sky-100 text-sky-700'}`}>
                              {hs.ipv6_mode}
                            </span>
                            {hs.ipv6_subnet && <span className="text-slate-500">{hs.ipv6_subnet}</span>}
                            {hs.ndp_iface && (
                              <>
                                <span className="text-slate-300">|</span>
                                <span className="font-sans text-slate-400">NDP 响应器</span>
                                <span className="text-emerald-600">{hs.ndp_iface}</span>
                                {hs.ndp_subnets && <span className="text-slate-500">({hs.ndp_subnets})</span>}
                              </>
                            )}
                          </div>
                        )}
                      </div>

                      {/* 每容器使用率 */}
                      {nodeStats && nodeStats.vms.length > 0 && (
                        <div>
                          <div className="mb-2 text-xs font-medium text-slate-500">每容器使用率（实时）</div>
                          <div className="divide-y divide-slate-100 rounded-lg border border-slate-200">
                            {nodeStats.vms.map((v) => {
                              const pct = v.mem_limit_mb > 0 ? (v.mem_used_mb / v.mem_limit_mb) * 100 : 0
                              return (
                                <div key={v.name} className="flex items-center gap-3 px-3 py-2.5">
                                  <div className="min-w-0 flex-1">
                                    <div className="flex items-center justify-between gap-2">
                                      <span className="truncate font-mono text-xs text-slate-800">{v.name}</span>
                                      <span className="shrink-0 font-mono text-xs text-slate-400">
                                        CPU {v.cpu_percent.toFixed(1)}% · 内存 {fmtBytes(v.mem_used_mb * 1024 * 1024)}
                                      </span>
                                    </div>
                                    <div className="mt-1.5 flex items-center gap-2">
                                      <LiveBar value={v.cpu_percent} max={100} className="flex-1" />
                                      <span className="w-14 shrink-0 text-right font-mono text-[10px] text-muted">{pct.toFixed(0)}%</span>
                                    </div>
                                  </div>
                                  <Badge color={v.status === 'running' ? 'green' : 'slate'} dot>
                                    {v.status === 'running' ? '运行' : '停止'}
                                  </Badge>
                                </div>
                              )
                            })}
                          </div>
                        </div>
                      )}
                      {statsLoading && <p className="text-xs text-slate-400">正在采集实时数据…</p>}
                    </>
                  ) : (
                    <p className="text-xs text-slate-400">节点离线，无法获取实时资源数据</p>
                  )}
                </CardContent>
              </Card>

              {/* 防火墙管理 */}
              <Card>
                <CardHeader
                  title={<span>防火墙 <span className="text-xs font-normal text-slate-400">rfw eBPF</span></span>}
                  subtitle="管理该宿主机入站/出站规则：GeoIP 国家拦截、出站 SMTP 拦截、自定义端口规则"
                  right={
                    <Button size="sm" variant="sky" onClick={() => setFwOpen(true)}>
                      <ShieldIcon className="h-3.5 w-3.5" />
                      管理规则
                    </Button>
                  }
                />
                <CardContent className="text-sm text-slate-500">
                  点击「管理规则」查看并编辑 rfw 防火墙规则（需节点已安装 rfw 并在线）。
                </CardContent>
              </Card>

              {/* AGENT TOKEN */}
              <Card>
                <CardHeader title="AGENT TOKEN" subtitle="宿主机接入令牌" />
                <CardContent>
                  {detail ? (
                    <div className="flex items-center gap-2">
                      <code className="flex-1 rounded-md bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800 break-all">
                        {showToken ? detail.node.token : '••••••••••••••••••••••••••••'}
                      </code>
                      <button
                        onClick={() => setShowToken(!showToken)}
                        className="inline-flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2.5 py-1.5 text-xs font-bold text-ink shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none"
                      >
                        {showToken ? <EyeOffIcon className="h-3.5 w-3.5" /> : <EyeIcon className="h-3.5 w-3.5" />}
                        {showToken ? '隐藏' : '显示'}
                      </button>
                      <Button size="sm" variant="outline" onClick={() => { navigator.clipboard.writeText(detail.node.token).catch(() => {}); toast('Token 已复制') }}>复制</Button>
                    </div>
                  ) : (
                    <p className="text-sm text-slate-400">节点详情加载中</p>
                  )}
                </CardContent>
              </Card>

              {/* 套餐管理（绑定所选节点） */}
              <Card>
                <CardHeader
                  title={<span>套餐管理 <span className="text-xs font-normal text-slate-400">{selected.name} 的套餐</span></span>}
                  subtitle={`共 ${nodePackages.length} 个 · 上架后其他用户可购买`}
                  right={
                    <Button size="sm" onClick={openAddPackage}>
                      <PlusIcon className="h-3.5 w-3.5" />
                      添加套餐
                    </Button>
                  }
                />
                <CardContent>
                  {nodePackages.length === 0 ? (
                    <Empty text="该节点暂无套餐，点击「添加套餐」创建并上架" />
                  ) : (
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                      {nodePackages.map((p) => (
                        <div key={p.id} className="rounded-lg border border-slate-200 px-3 py-2.5">
                          <div className="flex items-center justify-between gap-2">
                            <span className="text-sm font-medium text-slate-800">{p.name}</span>
                            <span className="font-mono text-sm font-semibold text-brand">${(p.price_cents / 100).toFixed(2)}/月</span>
                          </div>
                          <div className="mt-1 font-mono text-xs text-slate-400">
                            {p.cpu_cores}C / {p.memory_mb}M / {p.disk_mb}M · {p.traffic_gb === 0 ? '无限' : `${p.traffic_gb}GB`}
                          </div>
                          {p.images && (
                            <div className="mt-1 flex flex-wrap gap-1">
                              {p.images.split(',').filter(Boolean).map((ref) => (
                                <span key={ref} className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] text-slate-500">{ref}</span>
                              ))}
                            </div>
                          )}
                          <div className="mt-2.5 flex items-center justify-between">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="text-xs text-slate-400">上架</span>
                              <Switch checked={p.listed} onChange={() => toggleListed(p)} />
                              {p.early_full_refund && (
                                <span className="rounded bg-lavender px-1.5 py-0.5 text-[10px] font-bold text-black">
                                  早期全额退款
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-1">
                              <Button size="sm" variant="sage" onClick={() => openEditPackage(p)}>编辑</Button>
                              <Button size="sm" variant="ghost" className="text-red-600 hover:bg-red-50" onClick={() => removePkg(p)}>删除</Button>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* 优惠码（绑定所选节点） */}
              <Card>
                <CardHeader
                  title={<span>优惠码 <span className="text-xs font-normal text-slate-400">Purchase Coupons</span></span>}
                  subtitle="购买折扣码：买家创建实例时输入可抵扣"
                  right={
                    <Button size="sm" variant="outline" onClick={openAddCoupon}>
                      <PlusIcon className="h-3.5 w-3.5" />
                      创建优惠码
                    </Button>
                  }
                />
                <CardContent>
                  {nodeCoupons.length === 0 ? (
                    <p className="py-4 text-center text-sm text-slate-400">暂无优惠码</p>
                  ) : (
                    <div className="divide-y divide-slate-100">
                      {nodeCoupons.map((c) => (
                        <div key={c.id} className="flex items-center justify-between gap-3 py-2.5">
                          <div>
                            <span className="font-mono text-sm font-semibold text-slate-800">{c.code}</span>
                            <span className="ml-2 text-xs text-slate-400">
                              购买减 {c.percent_off}% · 已用 {c.used_count}/{c.max_uses === 0 ? '∞' : c.max_uses}
                              {c.expires_at ? ` · 到期 ${fmtDate(c.expires_at)}` : ''}
                            </span>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge color={c.enabled ? 'green' : 'slate'}>{c.enabled ? '有效' : '停用'}</Badge>
                            <Button size="sm" variant="sage" onClick={() => openEditCoupon(c)}>编辑</Button>
                            <Button size="sm" variant="ghost" className="text-red-600 hover:bg-red-50" onClick={() => removeCp(c)}>删除</Button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* 节点实例 */}
              <Card>
                <CardHeader title={<span>节点实例 <span className="text-xs font-normal text-slate-400">共 {nodeInstances.length} 台</span></span>} />
                <CardContent>
                  {nodeInstances.length === 0 ? (
                    <p className="py-4 text-center text-sm text-slate-400">该节点暂无实例</p>
                  ) : (
                    <div className="divide-y divide-slate-100">
                      {nodeInstances.map((inst) => (
                        <div key={inst.id} className="flex items-center justify-between gap-3 py-2.5">
                          <div className="min-w-0">
                            <Link to={`/instances/${inst.id}`} className="truncate text-sm font-medium text-slate-800 hover:text-brand">
                              {inst.display_name || inst.name}
                            </Link>
                            <div className="font-mono text-xs text-slate-400">{inst.ip}</div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge color={inst.status === 'running' ? 'green' : 'slate'} dot>
                              {inst.status === 'running' ? '运行中' : '已停止'}
                            </Badge>
                            <span className="text-xs text-slate-400">${(inst.price_cents / 100).toFixed(2)}/月</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          )}
        </div>
      </div>

      {/* 添加机器弹窗 */}
      <Modal
        open={hostOpen}
        title="添加机器"
        onClose={() => setHostOpen(false)}
        footer={
          hostResult ? (
            <Button onClick={() => setHostOpen(false)}>关闭</Button>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setHostOpen(false)}>取消</Button>
              <Button onClick={submitHost} disabled={hostLoading || !hostName}>
                {hostLoading ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '生成安装包'}
              </Button>
            </>
          )
        }
      >
        {hostResult ? (
          <div className="space-y-4">
            <p className="text-xs text-slate-500">
              在宿主机以 root 执行以下命令即可完成接入（若未内嵌 Token，装好后打开面板粘贴下方 Token）。
            </p>
            <div>
              <div className="mb-1.5 text-xs font-medium text-slate-400">节点 Token</div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-md bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800 break-all">
                  {hostResult.token}
                </code>
                <Button size="sm" variant="outline" onClick={() => { navigator.clipboard.writeText(hostResult.token).catch(() => {}); toast('Token 已复制') }}>复制</Button>
              </div>
            </div>
            <div>
              <div className="mb-1.5 text-xs font-medium text-slate-400">安装脚本</div>
              <pre className="narwhal-scroll max-h-96 overflow-auto rounded-lg bg-slate-50 p-4 font-mono text-xs whitespace-pre-wrap break-all">
                {hostResult.script}
              </pre>
              <Button size="sm" variant="outline" className="mt-2" onClick={() => { navigator.clipboard.writeText(hostResult.script).catch(() => {}); toast('脚本已复制') }}>
                复制脚本
              </Button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="节点名称">
              <Input value={hostName} onChange={(e) => setHostName(e.target.value)} placeholder="例如：香港 1 号机" />
            </Field>
            <Field label="宿主机地址" hint="留空则安装后由宿主机自动回传地址（需内嵌 Token）">
              <div className="flex gap-2">
                <Input
                  value={hostAddr}
                  onChange={(e) => setHostAddr(e.target.value)}
                  placeholder="http://1.2.3.4:8792 · 支持 IPv4/IPv6 · 留空自动获取"
                />
                <Button type="button" size="sm" variant="outline" className="shrink-0" onClick={runDetect} disabled={autoDetecting}>
                  {autoDetecting ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-blue-600" /> : '自动获取'}
                </Button>
              </div>
            </Field>
            <Field label="地区" hint="留空则接入后按 IP 自动识别">
              <Input value={hostRegion} onChange={(e) => setHostRegion(e.target.value)} placeholder="例如：香港" />
            </Field>
            <Field label="虚拟化类型">
              <Select value={hostVirtType} onChange={(e) => setHostVirtType(e.target.value)}>
                <option value="incus">Incus / LXD</option>
                <option value="oci">Podman / Docker</option>
              </Select>
            </Field>
            <label className="flex items-center gap-2 text-sm text-slate-600">
              <input
                type="checkbox"
                checked={hostWithToken}
                onChange={(e) => setHostWithToken(e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 accent-blue-600"
              />
              在脚本中内嵌 Token（安装即上线）
            </label>
            <Field label="Web 管理密码">
              <Input value={hostWebPassword} onChange={(e) => setHostWebPassword(e.target.value)} placeholder="留空自动生成" />
            </Field>
            {hostError && <p className="text-sm text-red-600">{hostError}</p>}
          </div>
        )}
      </Modal>

      {/* 添加/编辑套餐弹窗 */}
      <Modal
        open={pkgOpen}
        title={editingPkg ? `编辑套餐「${editingPkg.name}」` : '添加套餐'}
        subtitle={selected ? `绑定节点：${selected.name}` : ''}
        onClose={() => setPkgOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPkgOpen(false)}>取消</Button>
            <Button onClick={submitPackage} disabled={pkgSaving || !pkgName}>
              {pkgSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : editingPkg ? '保存' : '添加'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="套餐名称">
            <Input value={pkgName} onChange={(e) => setPkgName(e.target.value)} placeholder="例如：小鸡 1C512M" />
          </Field>
          <Field label="可选镜像" hint="买家只能从这些镜像中选择（不选则买家可自由选）">
            <div className="grid grid-cols-2 gap-2">
              {images.map((img) => (
                <label
                  key={img.id}
                  className={cn(
                    'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors',
                    pkgImgRefs.includes(img.ref)
                      ? 'border-brand bg-blue-50/60 text-brand'
                      : 'border-slate-200 text-body hover:border-slate-300',
                  )}
                >
                  <input type="checkbox" className="accent-brand" checked={pkgImgRefs.includes(img.ref)} onChange={() => togglePkgImage(img.ref)} />
                  <span>{img.name}</span>
                  <span className="font-mono text-[11px] text-muted">{img.ref}</span>
                </label>
              ))}
            </div>
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="CPU 核心数">
              <Input type="number" min={0} value={pkgCpu} onChange={(e) => setPkgCpu(e.target.value)} />
            </Field>
            <Field label="内存 (MB)">
              <Input type="number" min={0} value={pkgMem} onChange={(e) => setPkgMem(e.target.value)} />
            </Field>
            <Field label="磁盘 (MB)">
              <Input type="number" min={0} value={pkgDisk} onChange={(e) => setPkgDisk(e.target.value)} placeholder="如 5120 = 5GB" />
            </Field>
            <Field label="流量 (GB)">
              <Input type="number" min={0} value={pkgTraffic} onChange={(e) => setPkgTraffic(e.target.value)} />
            </Field>
            <Field label="端口槽位">
              <Input type="number" min={0} value={pkgSlots} onChange={(e) => setPkgSlots(e.target.value)} />
            </Field>
            <Field label="价格 (分)">
              <Input type="number" min={0} value={pkgPrice} onChange={(e) => setPkgPrice(e.target.value)} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={pkgIpv6}
              onChange={(e) => setPkgIpv6(e.target.checked)}
              className="h-4 w-4 rounded border-slate-300 accent-blue-600"
            />
            支持 IPv6
          </label>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={pkgListed}
              onChange={(e) => setPkgListed(e.target.checked)}
              className="h-4 w-4 rounded border-slate-300 accent-blue-600"
            />
            创建后立即上架（买家可见可购）
          </label>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={pkgEarlyRefund}
              onChange={(e) => setPkgEarlyRefund(e.target.checked)}
              className="h-4 w-4 rounded border-slate-300 accent-blue-600"
            />
            允许早期全额退款（买家取消机器时，开通≤1小时且流量≤1G 的实例返还全部余额）
          </label>
          {pkgError && <p className="text-sm text-red-600">{pkgError}</p>}
        </div>
      </Modal>

      {/* 创建/编辑优惠码弹窗 */}
      <Modal
        open={cpOpen}
        title={editingCp ? `编辑优惠码「${editingCp.code}」` : '创建优惠码'}
        subtitle={selected ? `绑定节点：${selected.name}` : ''}
        onClose={() => setCpOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCpOpen(false)}>取消</Button>
            <Button onClick={submitCoupon} disabled={cpSaving || (!editingCp && (!cpCode || !cpPercent))}>
              {cpSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : editingCp ? '保存' : '创建'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="优惠码" hint={editingCp ? '编码不可修改' : '买家购买本节点套餐时可输入抵扣'}>
            <Input value={cpCode} disabled={!!editingCp} onChange={(e) => setCpCode(e.target.value)} placeholder="例如：NARWHAL10" />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="折扣比例 (%)">
              <Input type="number" min={1} max={100} value={cpPercent} onChange={(e) => setCpPercent(e.target.value)} />
            </Field>
            <Field label="最大使用次数" hint="0 表示不限">
              <Input type="number" min={0} value={cpMaxUses} onChange={(e) => setCpMaxUses(e.target.value)} />
            </Field>
          </div>
          <Field label="到期时间" hint="留空 = 永不过期">
            <Input type="datetime-local" value={cpExpires} onChange={(e) => setCpExpires(e.target.value)} />
          </Field>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={cpEnabled}
              onChange={(e) => setCpEnabled(e.target.checked)}
              className="h-4 w-4 rounded border-slate-300 accent-blue-600"
            />
            启用
          </label>
          {cpError && <p className="text-sm text-red-600">{cpError}</p>}
        </div>
      </Modal>

      {/* 节点自检弹窗 */}
      <Modal
        open={sc.open}
        size="lg"
        title={`节点自检 - ${sc.node?.name ?? ''}`}
        subtitle={
          sc.run
            ? sc.run.status === 'running'
              ? '正在节点上运行 verify-ndp.sh（创建临时 IPv6 实例，镜像拉取可能需要数分钟）...'
              : sc.run.status === 'failed'
                ? '自检完成：存在失败项'
                : '自检完成：全部通过'
            : '正在触发远程自检...'
        }
        onClose={sc.close}
        footer={<Button variant="ghost" onClick={sc.close}>关闭</Button>}
      >
        <div className="space-y-3">
          {sc.error && <p className="text-sm text-red-600">{sc.error}</p>}
          {sc.run && (
            <div className="flex items-center gap-2">
              <Badge dot color={sc.run.status === 'running' ? 'blue' : sc.run.status === 'done' ? 'green' : 'red'}>
                {sc.run.status === 'running' ? '运行中' : sc.run.status === 'done' ? '通过' : '失败'}
              </Badge>
              <span className="font-mono text-xs text-slate-400">run {sc.run.runId}</span>
              {sc.run.status !== 'running' && (
                <span className="text-xs text-slate-400">退出码 {sc.run.status === 'failed' ? '非 0（详见下方输出）' : '0'}</span>
              )}
            </div>
          )}
          <div className="relative">
            <div className="narwhal-scroll max-h-[28rem] overflow-auto rounded-lg bg-slate-900 p-4">
              <pre className="font-mono text-xs leading-relaxed whitespace-pre-wrap text-slate-100">
                {sc.output || '（等待输出...）'}
              </pre>
              <div ref={sc.bottomRef} />
            </div>
            {/* 复制按钮固定在终端右上角（放在滚动容器外，不随内容滚动） */}
            {sc.output && (
              <CopyButton
                value={sc.output}
                className="absolute right-2 top-2 z-10"
              />
            )}
          </div>
        </div>
      </Modal>

      {/* 节点防火墙 */}
      <FirewallManager
        node={selected}
        open={fwOpen}
        onClose={() => setFwOpen(false)}
      />

      {/* 删除确认 */}
      <ConfirmDialog
        open={!!confirm}
        title={confirm?.title ?? ''}
        message={confirm?.message ?? ''}
        onConfirm={confirm ? confirm.action : async () => false}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}
