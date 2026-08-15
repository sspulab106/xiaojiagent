import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../lib/api'
import { parseSelfCheckMeta, useNodeSelfCheck } from '../lib/useNodeSelfCheck'
import { detectIpInfo } from '../lib/geo'
import type { Coupon, GiftCard, ImagePreset, Node, Package, PlatformSettings, User, UserDetail } from '../lib/types'
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
  Tabs,
  cn,
  fmtDate,
  useToast,
} from '../components/ui'
import { PlusIcon, TagIcon } from '../components/icons'
import FirewallManager from '../components/FirewallManager'

type Tab = 'nodes' | 'users' | 'packages' | 'coupons' | 'giftcards' | 'settings'

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



export default function Admin() {
  const toast = useToast()
  const [tab, setTab] = useState<Tab>('nodes')
  const [nodes, setNodes] = useState<Node[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [packages, setPackages] = useState<Package[]>([])
  const [coupons, setCoupons] = useState<Coupon[]>([])
  const [giftCards, setGiftCards] = useState<GiftCard[]>([])
  const [pkgImages, setPkgImages] = useState<ImagePreset[]>([])

  // 平台设置（SMTP 发件邮箱 / 邮箱验证 / Cloudflare 人机验证预留）
  const [settings, setSettings] = useState<PlatformSettings | null>(null)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsError, setSettingsError] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // 添加节点
  const [nodeOpen, setNodeOpen] = useState(false)
  const [nodeSaving, setNodeSaving] = useState(false)
  const [nodeError, setNodeError] = useState('')
  const [nodeName, setNodeName] = useState('')
  const [nodeRegion, setNodeRegion] = useState('')
  const [nodeAddr, setNodeAddr] = useState('')
  const [nodeToken, setNodeToken] = useState('')
  const [nodeAutoDetecting, setNodeAutoDetecting] = useState(false)

  // 生成安装脚本
  const [scriptNode, setScriptNode] = useState<Node | null>(null)
  const [scriptOpen, setScriptOpen] = useState(false)
  const [scriptVirtType, setScriptVirtType] = useState('incus')
  const [scriptWebPassword, setScriptWebPassword] = useState('')
  const [scriptWithToken, setScriptWithToken] = useState(true)
  const [scriptLoading, setScriptLoading] = useState(false)
  const [scriptError, setScriptError] = useState('')
  const [script, setScript] = useState<string | null>(null)

  // 新增套餐
  const [pkgOpen, setPkgOpen] = useState(false)
  const [pkgSaving, setPkgSaving] = useState(false)
  const [pkgError, setPkgError] = useState('')
  const [pkgName, setPkgName] = useState('')
  const [pkgImgRefs, setPkgImgRefs] = useState<string[]>([])
  const [pkgCpu, setPkgCpu] = useState('')
  const [pkgMem, setPkgMem] = useState('')
  const [pkgDisk, setPkgDisk] = useState('')
  const [pkgTraffic, setPkgTraffic] = useState('')
  const [pkgSlots, setPkgSlots] = useState('')
  const [pkgIpv6, setPkgIpv6] = useState(false)
  const [pkgPrice, setPkgPrice] = useState('')
  const [pkgListed, setPkgListed] = useState(true)
  const [pkgEarlyRefund, setPkgEarlyRefund] = useState(false)

  // 套餐列表：早期全额退款筛选 + 批量开关
  const [pkgRefundFilter, setPkgRefundFilter] = useState<'all' | 'on' | 'off'>('all')

  // 新增优惠码
  const [cpOpen, setCpOpen] = useState(false)
  const [cpSaving, setCpSaving] = useState(false)
  const [cpError, setCpError] = useState('')
  const [cpCode, setCpCode] = useState('')
  const [cpPercent, setCpPercent] = useState('')
  const [cpMaxUses, setCpMaxUses] = useState('')
  const [cpEnabled, setCpEnabled] = useState(true)
  const [cpExpires, setCpExpires] = useState('')

  // 编辑优惠码
  const [editCp, setEditCp] = useState<Coupon | null>(null)
  const [editCpOpen, setEditCpOpen] = useState(false)
  const [editCpSaving, setEditCpSaving] = useState(false)
  const [editCpError, setEditCpError] = useState('')
  const [editCpPercent, setEditCpPercent] = useState('')
  const [editCpMaxUses, setEditCpMaxUses] = useState('')
  const [editCpEnabled, setEditCpEnabled] = useState(true)
  const [editCpExpires, setEditCpExpires] = useState('')

  // 新增礼品卡
  const [gcOpen, setGcOpen] = useState(false)
  const [gcSaving, setGcSaving] = useState(false)
  const [gcError, setGcError] = useState('')
  const [gcAmount, setGcAmount] = useState('500')
  const [gcCount, setGcCount] = useState('1')
  const [gcCode, setGcCode] = useState('')
  const [gcExpires, setGcExpires] = useState('')

  // 编辑礼品卡
  const [editGc, setEditGc] = useState<GiftCard | null>(null)
  const [editGcOpen, setEditGcOpen] = useState(false)
  const [editGcSaving, setEditGcSaving] = useState(false)
  const [editGcError, setEditGcError] = useState('')
  const [editGcAmount, setEditGcAmount] = useState('')
  const [editGcExpires, setEditGcExpires] = useState('')

  // 用户管理：封禁 / 重置密码 / 调整余额 / 详情
  const [pwUser, setPwUser] = useState<User | null>(null)
  const [pwOpen, setPwOpen] = useState(false)
  const [pwSaving, setPwSaving] = useState(false)
  const [pwError, setPwError] = useState('')
  const [pwValue, setPwValue] = useState('')
  const [balUser, setBalUser] = useState<User | null>(null)
  const [balOpen, setBalOpen] = useState(false)
  const [balSaving, setBalSaving] = useState(false)
  const [balError, setBalError] = useState('')
  const [balDelta, setBalDelta] = useState('')
  const [balRemark, setBalRemark] = useState('')
  const [detailUser, setDetailUser] = useState<User | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailData, setDetailData] = useState<UserDetail | null>(null)

  // 节点防火墙
  const [fwNode, setFwNode] = useState<Node | null>(null)

  // 节点自检（远程运行 verify-ndp.sh，轮询输出）—— 管理后台 admin 端点
  const sc = useNodeSelfCheck(true)

  // 删除确认（节点 / 套餐 / 优惠码共用）
  const [confirm, setConfirm] = useState<{ title: string; message: string; action: () => Promise<boolean> } | null>(null)

  const loadAll = useCallback(async () => {
    try {
      const [n, u, p, c, g, imgs, s] = await Promise.all([
        api.adminNodes(),
        api.adminUsers(),
        api.packages(),
        api.adminCoupons(),
        api.adminGiftCards(),
        api.images(),
        api.settings().catch(() => null),
      ])
      setNodes(n)
      setUsers(u)
      setPackages(p)
      setCoupons(c)
      setGiftCards(g)
      setPkgImages(imgs)
      if (s) setSettings(s)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadAll()
  }, [loadAll])

  const openAddNode = () => {
    setNodeError('')
    setNodeName('')
    setNodeRegion('')
    setNodeAddr('')
    setNodeToken('')
    setNodeOpen(true)
  }

  const submitNode = async () => {
    setNodeSaving(true)
    setNodeError('')
    try {
      await api.addNode({ name: nodeName, agent_addr: nodeAddr || undefined, token: nodeToken, region: nodeRegion || undefined })
      setNodeOpen(false)
      toast('节点已添加')
      await loadAll()
    } catch (err) {
      setNodeError(err instanceof ApiError ? err.message : '添加节点失败')
    } finally {
      setNodeSaving(false)
    }
  }

  const runNodeDetect = async () => {
    setNodeAutoDetecting(true)
    setNodeError('')
    try {
      const info = await detectIpInfo()
      if (info.ipv4) {
        setNodeAddr(`http://${info.ipv4}:8792`)
      } else if (info.ipv6) {
        setNodeAddr(`http://[${info.ipv6}]:8792`)
      }
      if (info.region) {
        setNodeRegion(info.region)
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
      setNodeAutoDetecting(false)
    }
  }

  const removeNode = (node: Node) => {
    setConfirm({
      title: '删除节点',
      message: `确定删除节点「${node.name}」吗？`,
      action: async () => {
        try {
          await api.deleteNode(node.id)
          toast('节点已删除')
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '删除节点失败')
          return false
        }
      },
    })
  }

  const openScript = (node: Node) => {
    setScriptNode(node)
    setScriptOpen(true)
    setScriptVirtType('incus')
    setScriptWebPassword('')
    setScriptWithToken(true)
    setScriptError('')
    setScript(null)
  }

  const submitScript = async () => {
    if (!scriptNode) return
    setScriptLoading(true)
    setScriptError('')
    try {
      const res = await api.nodeInstallScript(scriptNode.id, {
        with_token: scriptWithToken,
        web_password: scriptWebPassword || undefined,
        virt_type: scriptVirtType,
      })
      setScript(res.script)
    } catch (err) {
      setScriptError(err instanceof ApiError ? err.message : '生成脚本失败')
    } finally {
      setScriptLoading(false)
    }
  }

  const extendUser = async (u: User) => {
    try {
      await api.extendUser(u.id, 30)
      toast('已延期 30 天')
      await loadAll()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '延期失败')
    }
  }

  const togglePkgImage = (ref: string) => {
    setPkgImgRefs((prev) => (prev.includes(ref) ? prev.filter((r) => r !== ref) : [...prev, ref]))
  }

  const openAddPackage = () => {
    setPkgError('')
    setPkgName('')
    setPkgImgRefs([])
    setPkgCpu('')
    setPkgMem('')
    setPkgDisk('')
    setPkgTraffic('')
    setPkgSlots('')
    setPkgIpv6(false)
    setPkgPrice('')
    setPkgListed(true)
    setPkgEarlyRefund(false)
    setPkgOpen(true)
  }

  const submitPackage = async () => {
    setPkgSaving(true)
    setPkgError('')
    try {
      await api.addPackage({
        name: pkgName,
        images: pkgImgRefs.join(','),
        cpu_cores: Number(pkgCpu),
        memory_mb: Number(pkgMem),
        disk_mb: Number(pkgDisk),
        traffic_gb: Number(pkgTraffic),
        port_slots: Number(pkgSlots),
        ipv6: pkgIpv6,
        price_cents: Number(pkgPrice),
        listed: pkgListed,
        early_full_refund: pkgEarlyRefund,
      })
      setPkgOpen(false)
      toast('套餐已添加')
      await loadAll()
    } catch (err) {
      setPkgError(err instanceof ApiError ? err.message : '新增套餐失败')
    } finally {
      setPkgSaving(false)
    }
  }

  const removePackage = (p: Package) => {
    setConfirm({
      title: '删除套餐',
      message: `确定删除套餐「${p.name}」吗？`,
      action: async () => {
        try {
          await api.deletePackage(p.id)
          toast('套餐已删除')
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '删除套餐失败')
          return false
        }
      },
    })
  }

  // 批量开关「允许早期全额退款」：作用于当前筛选结果
  const batchToggleEarlyRefund = (target: boolean, count: number) => {
    setConfirm({
      title: target ? '批量开启早期全额退款' : '批量关闭早期全额退款',
      message: `将对筛选出的 ${count} 个套餐${target ? '开启' : '关闭'}「允许早期全额退款」${target ? '。开启后，这些套餐下开通≤1小时且流量≤1G 的实例取消时可返还全部余额。' : '。关闭后，这些套餐下取消机器一律不退款。'}`,
      action: async () => {
        try {
          const filtered = packages.filter((p) =>
            pkgRefundFilter === 'all' ? true : pkgRefundFilter === 'on' ? p.early_full_refund : !p.early_full_refund,
          )
          await Promise.all(filtered.map((p) => api.updatePackage(p.id, { early_full_refund: target })))
          toast(`已${target ? '开启' : '关闭'} ${filtered.length} 个套餐的早期全额退款`)
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '批量更新失败')
          return false
        }
      },
    })
  }

  const openAddCoupon = () => {
    setCpError('')
    setCpCode('')
    setCpPercent('')
    setCpMaxUses('')
    setCpEnabled(true)
    setCpExpires('')
    setCpOpen(true)
  }

  const submitCoupon = async () => {
    setCpSaving(true)
    setCpError('')
    try {
      await api.addCoupon({
        code: cpCode,
        percent_off: Number(cpPercent),
        max_uses: Number(cpMaxUses) || 0,
        enabled: cpEnabled,
        expires_at: fromLocalInput(cpExpires) ?? undefined,
      })
      setCpOpen(false)
      toast('优惠码已创建')
      await loadAll()
    } catch (err) {
      setCpError(err instanceof ApiError ? err.message : '创建优惠码失败')
    } finally {
      setCpSaving(false)
    }
  }

  const removeCoupon = (c: Coupon) => {
    setConfirm({
      title: '删除优惠码',
      message: `确定删除优惠码「${c.code}」吗？`,
      action: async () => {
        try {
          await api.deleteCoupon(c.id)
          toast('优惠码已删除')
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '删除优惠码失败')
          return false
        }
      },
    })
  }

  const openEditCoupon = (c: Coupon) => {
    setEditCp(c)
    setEditCpError('')
    setEditCpPercent(String(c.percent_off))
    setEditCpMaxUses(String(c.max_uses))
    setEditCpEnabled(c.enabled)
    setEditCpExpires(toLocalInput(c.expires_at))
    setEditCpOpen(true)
  }

  const submitEditCoupon = async () => {
    if (!editCp) return
    setEditCpSaving(true)
    setEditCpError('')
    try {
      await api.updateCoupon(editCp.id, {
        percent_off: editCpPercent.trim() !== '' ? Number(editCpPercent) : undefined,
        max_uses: editCpMaxUses.trim() !== '' ? Number(editCpMaxUses) : undefined,
        enabled: editCpEnabled,
        expires_at: fromLocalInput(editCpExpires) ?? undefined,
        clear_expiry: editCpExpires.trim() === '' && !!editCp.expires_at,
      })
      setEditCpOpen(false)
      toast('优惠码已更新')
      await loadAll()
    } catch (err) {
      setEditCpError(err instanceof ApiError ? err.message : '更新优惠码失败')
    } finally {
      setEditCpSaving(false)
    }
  }

  const openAddGiftCards = () => {
    setGcError('')
    setGcAmount('500')
    setGcCount('1')
    setGcCode('')
    setGcExpires('')
    setGcOpen(true)
  }

  const submitGiftCards = async () => {
    setGcSaving(true)
    setGcError('')
    try {
      await api.addGiftCards({
        code: gcCode.trim() || undefined,
        amount_cents: Number(gcAmount) || 0,
        count: Number(gcCount) || 1,
        expires_at: fromLocalInput(gcExpires) ?? undefined,
      })
      setGcOpen(false)
      toast('兑换码已生成')
      await loadAll()
    } catch (err) {
      setGcError(err instanceof ApiError ? err.message : '生成兑换码失败')
    } finally {
      setGcSaving(false)
    }
  }

  const removeGiftCard = (g: GiftCard) => {
    setConfirm({
      title: '删除兑换码',
      message: `确定删除兑换码「${g.code}」吗？`,
      action: async () => {
        try {
          await api.deleteGiftCard(g.id)
          toast('兑换码已删除')
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '删除兑换码失败')
          return false
        }
      },
    })
  }

  const openEditGiftCard = (g: GiftCard) => {
    setEditGc(g)
    setEditGcError('')
    setEditGcAmount(String(g.amount_cents))
    setEditGcExpires(toLocalInput(g.expires_at))
    setEditGcOpen(true)
  }

  const submitEditGiftCard = async () => {
    if (!editGc) return
    setEditGcSaving(true)
    setEditGcError('')
    try {
      await api.updateGiftCard(editGc.id, {
        amount_cents: editGcAmount.trim() !== '' ? Number(editGcAmount) : undefined,
        expires_at: fromLocalInput(editGcExpires) ?? undefined,
        clear_expiry: editGcExpires.trim() === '' && !!editGc.expires_at,
      })
      setEditGcOpen(false)
      toast('兑换码已更新')
      await loadAll()
    } catch (err) {
      setEditGcError(err instanceof ApiError ? err.message : '更新兑换码失败')
    } finally {
      setEditGcSaving(false)
    }
  }

  // ---- 用户管理 ----

  const openResetPassword = (u: User) => {
    setPwUser(u)
    setPwError('')
    setPwValue('')
    setPwOpen(true)
  }

  const submitResetPassword = async () => {
    if (!pwUser) return
    setPwSaving(true)
    setPwError('')
    try {
      await api.resetUserPassword(pwUser.id, pwValue)
      setPwOpen(false)
      toast(`已重置 ${pwUser.username} 的登录密码`)
    } catch (err) {
      setPwError(err instanceof ApiError ? err.message : '重置密码失败')
    } finally {
      setPwSaving(false)
    }
  }

  const toggleBan = async (u: User) => {
    setConfirm({
      title: u.banned ? '解封用户' : '封禁用户',
      message: u.banned
        ? `确定解封「${u.username}」吗？解封后可正常登录。`
        : `确定封禁「${u.username}」吗？封禁后无法登录，所有接口请求将被拒绝。`,
      action: async () => {
        try {
          await api.banUser(u.id, !u.banned)
          toast(u.banned ? '用户已解封' : '用户已封禁')
          await loadAll()
          return true
        } catch (err) {
          setError(err instanceof ApiError ? err.message : '操作失败')
          return false
        }
      },
    })
  }

  const openAdjustBalance = (u: User) => {
    setBalUser(u)
    setBalError('')
    setBalDelta('')
    setBalRemark('')
    setBalOpen(true)
  }

  const submitAdjustBalance = async () => {
    if (!balUser) return
    setBalSaving(true)
    setBalError('')
    try {
      await api.adjustUserBalance(balUser.id, Math.round(Number(balDelta) * 100), balRemark.trim() || undefined)
      setBalOpen(false)
      toast('余额已调整')
      await loadAll()
    } catch (err) {
      setBalError(err instanceof ApiError ? err.message : '调整余额失败')
    } finally {
      setBalSaving(false)
    }
  }

  const openUserDetail = async (u: User) => {
    setDetailUser(u)
    setDetailOpen(true)
    setDetailLoading(true)
    setDetailData(null)
    try {
      const d = await api.adminUserDetail(u.id)
      setDetailData(d)
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '加载用户详情失败')
    } finally {
      setDetailLoading(false)
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

  const tabs: Array<{ key: Tab; label: string }> = [
    { key: 'nodes', label: '节点' },
    { key: 'users', label: '用户' },
    { key: 'packages', label: '套餐' },
    { key: 'coupons', label: '优惠码' },
    { key: 'giftcards', label: '礼品卡' },
    { key: 'settings', label: '平台设置' },
  ]

  const saveSettings = async () => {
    if (!settings) return
    setSettingsSaving(true)
    setSettingsError('')
    try {
      await api.updateSettings(settings)
      toast('平台设置已保存')
    } catch (err) {
      setSettingsError(err instanceof ApiError ? err.message : '保存失败')
    } finally {
      setSettingsSaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-lg font-semibold text-slate-900">
          管理后台 <span className="text-sm font-normal text-slate-400">Admin</span>
        </h1>
        <p className="mt-0.5 text-sm text-slate-500">节点、用户、套餐与优惠码管理。</p>
      </header>

      <Tabs
        value={tab}
        onChange={(v) => setTab(v as Tab)}
        options={tabs.map((t) => ({ key: t.key, label: t.label }))}
      />

      {/* 节点 */}
      {tab === 'nodes' && (
        <Card>
          <CardHeader
            title="节点管理"
            subtitle={`共 ${nodes.length} 个节点`}
            right={<Button size="sm" onClick={openAddNode}><PlusIcon className="h-3.5 w-3.5" />添加节点</Button>}
          />
          {nodes.length === 0 ? (
            <Empty text="暂无节点" />
          ) : (
            <CardContent className="overflow-x-auto px-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">名称</th>
                    <th className="px-5 py-3 font-medium">地区</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">Agent 地址</th>
                    <th className="px-5 py-3 font-medium">宿主机 IP</th>
                    <th className="px-5 py-3 font-medium">IPv6 模式</th>
                    <th className="px-5 py-3 font-medium">最近自检</th>
                    <th className="px-5 py-3 font-medium">配置</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {nodes.map((node) => (
                    <tr key={node.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3 font-medium text-slate-900">{node.name}</td>
                      <td className="px-5 py-3 text-slate-500">{node.region}</td>
                      <td className="px-5 py-3">
                        <Badge color={node.status === 'online' ? 'green' : 'red'} dot>
                          {node.status === 'online' ? '在线' : '离线'}
                        </Badge>
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{node.agent_addr}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{node.host_ip}</td>
                      <td className="px-5 py-3">
                        <div className="flex max-w-[15rem] flex-col items-start gap-1">
                          <Badge
                            color={node.ipv6_mode === 'subnet' ? 'purple' : node.ipv6_mode === 'snat' ? 'blue' : 'slate'}
                          >
                            {node.ipv6_mode === 'subnet'
                              ? 'subnet 子网'
                              : node.ipv6_mode === 'snat'
                                ? 'snat 共享'
                                : node.ipv6_mode === 'none'
                                  ? '未启用'
                                  : node.ipv6_mode
                                    ? node.ipv6_mode
                                    : '未检测'}
                          </Badge>
                          {node.ipv6_mode === 'subnet' && node.ipv6_subnet && (
                            <span className="truncate font-mono text-[11px] text-slate-400" title={`${node.ipv6_subnet}${node.ndp_iface ? ` · NDP ${node.ndp_iface}` : ''}`}>
                              {node.ipv6_subnet}
                              {node.ndp_iface ? ` · NDP ${node.ndp_iface}` : ''}
                            </span>
                          )}
                          {node.ipv6_mode === 'snat' && node.ipv6_subnet && (
                            <span className="truncate font-mono text-[11px] text-slate-400" title={node.ipv6_subnet}>{node.ipv6_subnet}</span>
                          )}
                        </div>
                      </td>
                      <td className="px-5 py-3">
                        {(() => {
                          const scm = parseSelfCheckMeta(node)
                          if (!scm || !scm.status) {
                            return <span className="text-xs text-slate-400">未自检</span>
                          }
                          if (scm.status === 'running') {
                            // 运行中快照可能因关闭弹窗/刷新页面而永不到达终态：
                            // 超过 Agent 15 分钟超时 + 缓冲即视为中断，避免徽章卡死在「运行中」
                            const stale =
                              !!scm.started_at &&
                              Date.now() - new Date(scm.started_at).getTime() > 16 * 60 * 1000
                            return stale ? (
                              <span title="自检已中断（超时未完成），可重新触发">
                                <Badge color="slate" dot>中断</Badge>
                              </span>
                            ) : (
                              <Badge color="blue" dot>运行中</Badge>
                            )
                          }
                          const ok = scm.status === 'done'
                          const tip = [
                            ok ? '最近自检通过' : '最近自检失败',
                            scm.finished_at ? fmtDate(scm.finished_at) : '',
                            `退出码 ${scm.exit_code ?? '?'}`,
                            scm.output ? scm.output.replace(/\s+/g, ' ').trim().slice(0, 180) : '',
                          ]
                            .filter(Boolean)
                            .join(' · ')
                          return (
                            <span title={tip}>
                              <Badge color={ok ? 'green' : 'red'} dot>{ok ? '通过' : '失败'}</Badge>
                            </span>
                          )
                        })()}
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">
                        {node.total_cores}C / {node.total_memory_mb}MB / {node.total_disk_mb}MB
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            size="sm"
                            variant="sage"
                            disabled={node.status !== 'online' || sc.starting}
                            onClick={() =>
                              sc.start(node, (st) => {
                                // 局部更新该节点徽章（避免全量 loadAll 在瞬时网络故障时把整页切到错误态）
                                setNodes((prev) =>
                                  prev.map((n) =>
                                    n.id === node.id
                                      ? {
                                          ...n,
                                          last_selfcheck: JSON.stringify({
                                            status: st.status,
                                            run_id: st.id,
                                            exit_code: st.exit_code,
                                            started_at: st.started_at,
                                            finished_at: st.finished_at,
                                            output: st.output,
                                          }),
                                        }
                                      : n,
                                  ),
                                )
                              })
                            }
                            title={node.status !== 'online' ? '节点离线，无法远程自检' : '在节点上运行 verify-ndp.sh 并回显结果'}
                          >
                            自检
                          </Button>
                          <Button size="sm" variant="sky" onClick={() => setFwNode(node)}>防火墙</Button>
                          <Button size="sm" variant="outline" onClick={() => openScript(node)}>安装脚本</Button>
                          <Button size="sm" variant="danger" onClick={() => removeNode(node)}>删除</Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          )}
        </Card>
      )}

      {/* 用户 */}
      {tab === 'users' && (
        <Card>
          <CardHeader title="用户管理" subtitle={`共 ${users.length} 个用户`} />
          {users.length === 0 ? (
            <Empty text="暂无用户" />
          ) : (
            <CardContent className="overflow-x-auto px-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">ID</th>
                    <th className="px-5 py-3 font-medium">用户名</th>
                    <th className="px-5 py-3 font-medium">角色</th>
                    <th className="px-5 py-3 font-medium">实例配额</th>
                    <th className="px-5 py-3 font-medium">余额</th>
                    <th className="px-5 py-3 font-medium">到期时间</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3 font-mono text-slate-500">{u.id}</td>
                      <td className="px-5 py-3 font-medium text-slate-900">{u.username}</td>
                      <td className="px-5 py-3">
                        <Badge color={u.role === 'admin' ? 'indigo' : 'slate'}>{u.role === 'admin' ? '管理员' : '普通用户'}</Badge>
                      </td>
                      <td className="px-5 py-3 text-slate-500">{u.instance_quota}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">${(u.balance_cents / 100).toFixed(2)}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{u.expires_at ? fmtDate(u.expires_at) : '未设置'}</td>
                      <td className="px-5 py-3">
                        {u.banned ? <Badge color="red">已封禁</Badge> : <Badge color="green">正常</Badge>}
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="outline" onClick={() => openUserDetail(u)}>详情</Button>
                          <Button size="sm" variant="outline" onClick={() => openResetPassword(u)}>重置密码</Button>
                          <Button size="sm" variant="sage" onClick={() => openAdjustBalance(u)}>调整余额</Button>
                          <Button size="sm" variant="outline" onClick={() => extendUser(u)}>延期30天</Button>
                          <Button size="sm" variant={u.banned ? 'sage' : 'danger'} onClick={() => toggleBan(u)}>
                            {u.banned ? '解封' : '封禁'}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          )}
        </Card>
      )}

      {/* 套餐 */}
      {tab === 'packages' && (() => {
        const filtered = packages.filter((p) =>
          pkgRefundFilter === 'all' ? true : pkgRefundFilter === 'on' ? p.early_full_refund : !p.early_full_refund,
        )
        // 当前筛选结果中「全部已开启」→ 提供批量关闭；否则提供批量开启
        const allOn = filtered.length > 0 && filtered.every((p) => p.early_full_refund)
        return (
        <Card>
          <CardHeader
            title="套餐管理"
            subtitle={`共 ${packages.length} 个套餐${pkgRefundFilter !== 'all' ? ` · 筛选出 ${filtered.length} 个` : ''}`}
            right={<Button size="sm" onClick={openAddPackage}><PlusIcon className="h-3.5 w-3.5" />新增套餐</Button>}
          />
          {packages.length === 0 ? (
            <Empty text="暂无套餐" />
          ) : (
            <CardContent className="overflow-x-auto px-0">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-5 py-3">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-medium text-slate-400">早期全额退款</span>
                  <Select
                    value={pkgRefundFilter}
                    onChange={(e) => setPkgRefundFilter(e.target.value as 'all' | 'on' | 'off')}
                    className="h-8 w-28 text-xs"
                  >
                    <option value="all">全部</option>
                    <option value="on">仅已开启</option>
                    <option value="off">仅未开启</option>
                  </Select>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={filtered.length === 0}
                  onClick={() => batchToggleEarlyRefund(!allOn, filtered.length)}
                >
                  {allOn ? '批量关闭' : '批量开启'}（{filtered.length} 个）
                </Button>
              </div>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">名称</th>
                    <th className="px-5 py-3 font-medium">类型</th>
                    <th className="px-5 py-3 font-medium">镜像</th>
                    <th className="px-5 py-3 font-medium">配置</th>
                    <th className="px-5 py-3 font-medium">流量</th>
                    <th className="px-5 py-3 font-medium">价格(分)</th>
                    <th className="px-5 py-3 font-medium">上架</th>
                    <th className="px-5 py-3 font-medium">早期全额退款</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((p) => (
                    <tr key={p.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3 font-medium text-slate-900">{p.name}</td>
                      <td className="px-5 py-3">
                        <Badge color={p.node_id === 0 ? 'indigo' : 'blue'}>{p.node_id === 0 ? '平台' : p.node_name || '机主'}</Badge>
                      </td>
                      <td className="px-5 py-3">
                        {p.images ? (
                          <span className="max-w-[10rem] truncate font-mono text-xs text-slate-500">{p.images}</span>
                        ) : (
                          <span className="text-xs text-slate-400">任意</span>
                        )}
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">
                        {p.cpu_cores}C / {p.memory_mb}M / {p.disk_mb}M / {p.port_slots}槽
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{p.traffic_gb === 0 ? '无限' : `${p.traffic_gb} GB`}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{p.price_cents}</td>
                      <td className="px-5 py-3">
                        <Badge color={p.listed ? 'green' : 'slate'}>{p.listed ? '已上架' : '未上架'}</Badge>
                      </td>
                      <td className="px-5 py-3">
                        <Badge color={p.early_full_refund ? 'purple' : 'slate'}>
                          {p.early_full_refund ? '开启' : '关闭'}
                        </Badge>
                      </td>
                      <td className="px-5 py-3 text-right">
                        <Button size="sm" variant="danger" onClick={() => removePackage(p)}>删除</Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          )}
        </Card>
        )
      })()}

      {/* 优惠码 */}
      {tab === 'coupons' && (
        <Card>
          <CardHeader
            title={<span>优惠码 <span className="text-xs font-normal text-slate-400">Top-up Coupons</span></span>}
            subtitle={`共 ${coupons.length} 个优惠码`}
            right={<Button size="sm" onClick={openAddCoupon}><PlusIcon className="h-3.5 w-3.5" />新增优惠码</Button>}
          />
          {coupons.length === 0 ? (
            <Empty text="暂无优惠码" icon={<TagIcon className="h-8 w-8" />} />
          ) : (
            <CardContent className="overflow-x-auto px-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">编码</th>
                    <th className="px-5 py-3 font-medium">折扣比例</th>
                    <th className="px-5 py-3 font-medium">使用次数</th>
                    <th className="px-5 py-3 font-medium">到期时间</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">创建时间</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {coupons.map((c) => {
                    const expired = c.expires_at && new Date(c.expires_at).getTime() < Date.now()
                    return (
                    <tr key={c.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3 font-mono font-semibold text-slate-900">{c.code}</td>
                      <td className="px-5 py-3 font-mono text-blue-600">{c.percent_off}%</td>
                      <td className="px-5 py-3 font-mono text-slate-500">
                        {c.used_count}/{c.max_uses === 0 ? '∞' : c.max_uses}
                      </td>
                      <td className="px-5 py-3">
                        {c.expires_at ? (
                          <Badge color={expired ? 'red' : 'orange'}>{fmtDate(c.expires_at)}</Badge>
                        ) : (
                          <span className="text-xs text-slate-400">永不过期</span>
                        )}
                      </td>
                      <td className="px-5 py-3">
                        <Badge color={c.enabled ? 'green' : 'slate'}>{c.enabled ? '启用' : '停用'}</Badge>
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{fmtDate(c.created_at)}</td>
                      <td className="px-5 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="lemon" onClick={() => openEditCoupon(c)}>编辑</Button>
                          <Button size="sm" variant="danger" onClick={() => removeCoupon(c)}>删除</Button>
                        </div>
                      </td>
                    </tr>
                    )
                  })}
                </tbody>
              </table>
            </CardContent>
          )}
        </Card>
      )}

      {/* 礼品卡 / 兑换码 */}
      {tab === 'giftcards' && (
        <Card>
          <CardHeader
            title={<span>兑换码 <span className="text-xs font-normal text-slate-400">Gift Cards</span></span>}
            subtitle={`共 ${giftCards.length} 张 · 用户可在「账单充值」页兑换入账`}
            right={<Button size="sm" onClick={openAddGiftCards}><PlusIcon className="h-3.5 w-3.5" />发放兑换码</Button>}
          />
          {giftCards.length === 0 ? (
            <Empty text="暂无兑换码" icon={<TagIcon className="h-8 w-8" />} />
          ) : (
            <CardContent className="overflow-x-auto px-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">编码</th>
                    <th className="px-5 py-3 font-medium">面额</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">领取人</th>
                    <th className="px-5 py-3 font-medium">领取时间</th>
                    <th className="px-5 py-3 font-medium">到期时间</th>
                    <th className="px-5 py-3 font-medium">创建时间</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {giftCards.map((g) => {
                    const expired = g.expires_at && new Date(g.expires_at).getTime() < Date.now()
                    return (
                    <tr key={g.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                      <td className="px-5 py-3 font-mono font-semibold text-slate-900">{g.code}</td>
                      <td className="px-5 py-3 font-mono text-blue-600">${(g.amount_cents / 100).toFixed(2)}</td>
                      <td className="px-5 py-3">
                        <Badge color={g.status === 'redeemed' ? 'slate' : 'green'}>{g.status === 'redeemed' ? '已兑换' : '未兑换'}</Badge>
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{g.redeemed_by || '-'}</td>
                      <td className="px-5 py-3 font-mono text-slate-500">{g.redeemed_at ? fmtDate(g.redeemed_at) : '-'}</td>
                      <td className="px-5 py-3">
                        {g.expires_at ? (
                          <Badge color={expired ? 'red' : 'orange'}>{fmtDate(g.expires_at)}</Badge>
                        ) : (
                          <span className="text-xs text-slate-400">永不过期</span>
                        )}
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{fmtDate(g.created_at)}</td>
                      <td className="px-5 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="peach" disabled={g.status === 'redeemed'} onClick={() => openEditGiftCard(g)}>编辑</Button>
                          <Button size="sm" variant="danger" disabled={g.status === 'redeemed'} onClick={() => removeGiftCard(g)}>删除</Button>
                        </div>
                      </td>
                    </tr>
                    )
                  })}
                </tbody>
              </table>
            </CardContent>
          )}
        </Card>
      )}

      {/* 平台设置 */}
      {tab === 'settings' && (
        <Card>
          <CardHeader
            title="平台设置"
            subtitle="发件邮箱配置、邮箱验证开关与预留的人机验证开关"
            right={<Button size="sm" onClick={saveSettings} disabled={settingsSaving}>{settingsSaving ? '保存中…' : '保存设置'}</Button>}
          />
          <CardContent className="space-y-6">
            {settings ? (
              <>
                <div>
                  <p className="mb-2 text-sm font-semibold text-slate-800">SMTP 发件邮箱</p>
                  <p className="mb-3 text-xs text-slate-400">用于发送邮箱验证码等系统邮件，未配置时邮箱验证自动停用（绑定即视为已验证）</p>
                  <div className="grid gap-4 md:grid-cols-2">
                    <Field label="SMTP 服务器">
                      <Input value={settings.smtp_host} onChange={(e) => setSettings({ ...settings, smtp_host: e.target.value })} placeholder="smtp.example.com" />
                    </Field>
                    <Field label="端口">
                      <Input value={settings.smtp_port} onChange={(e) => setSettings({ ...settings, smtp_port: e.target.value })} placeholder="587" />
                    </Field>
                    <Field label="用户名">
                      <Input value={settings.smtp_user} onChange={(e) => setSettings({ ...settings, smtp_user: e.target.value })} placeholder="noreply@example.com" />
                    </Field>
                    <Field label="密码" hint="留空保持不变">
                      <Input type="password" value={settings.smtp_pass} onChange={(e) => setSettings({ ...settings, smtp_pass: e.target.value })} placeholder="SMTP 授权码" autoComplete="new-password" />
                    </Field>
                    <Field label="发件人地址" hint="留空则使用用户名">
                      <Input value={settings.smtp_from} onChange={(e) => setSettings({ ...settings, smtp_from: e.target.value })} placeholder="noreply@example.com" />
                    </Field>
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl border-2 border-slate-200 px-4 py-3">
                    <div>
                      <div className="text-sm font-semibold text-slate-800">开启邮箱验证</div>
                      <div className="text-xs text-slate-400">开启后用户绑定邮箱需输入邮件中的验证码完成验证</div>
                    </div>
                    <input
                      type="checkbox"
                      className="h-5 w-5 accent-blue-600"
                      checked={settings.email_verify_enabled === '1'}
                      onChange={(e) => setSettings({ ...settings, email_verify_enabled: e.target.checked ? '1' : '0' })}
                    />
                  </label>
                  <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl border-2 border-slate-200 px-4 py-3">
                    <div>
                      <div className="text-sm font-semibold text-slate-800">Cloudflare 人机验证（预留）</div>
                      <div className="text-xs text-slate-400">开关已预留，接入 Turnstile 后用于登录/注册人机校验</div>
                    </div>
                    <input
                      type="checkbox"
                      className="h-5 w-5 accent-blue-600"
                      checked={settings.cloudflare_captcha_enabled === '1'}
                      onChange={(e) => setSettings({ ...settings, cloudflare_captcha_enabled: e.target.checked ? '1' : '0' })}
                    />
                  </label>
                </div>
                {settingsError && <p className="text-sm text-red-600">{settingsError}</p>}
              </>
            ) : (
              <Empty text="无法加载平台设置" />
            )}
          </CardContent>
        </Card>
      )}

      {/* 添加节点弹窗 */}
      <Modal
        open={nodeOpen}
        title="添加节点"
        onClose={() => setNodeOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setNodeOpen(false)}>取消</Button>
            <Button onClick={submitNode} disabled={nodeSaving || !nodeName || !nodeToken}>
              {nodeSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '添加'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="节点名称">
            <Input value={nodeName} onChange={(e) => setNodeName(e.target.value)} placeholder="例如：香港 1 号机" />
          </Field>
          <Field label="地区" hint="留空则接入后按 IP 自动识别">
            <Input value={nodeRegion} onChange={(e) => setNodeRegion(e.target.value)} placeholder="例如：香港" />
          </Field>
          <Field label="Agent 地址" hint="留空则安装脚本装好后自动回传">
            <div className="flex gap-2">
              <Input
                value={nodeAddr}
                onChange={(e) => setNodeAddr(e.target.value)}
                placeholder="http://1.2.3.4:8792 · 支持 IPv4/IPv6 · 留空自动获取"
              />
              <Button type="button" size="sm" variant="outline" className="shrink-0" onClick={runNodeDetect} disabled={nodeAutoDetecting}>
                {nodeAutoDetecting ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-blue-600" /> : '自动获取'}
              </Button>
            </div>
          </Field>
          <Field label="Agent Token">
            <Input value={nodeToken} onChange={(e) => setNodeToken(e.target.value)} placeholder="节点注册令牌" />
          </Field>
          {nodeError && <p className="text-sm text-red-600">{nodeError}</p>}
        </div>
      </Modal>

      {/* 节点防火墙弹窗 */}
      <FirewallManager
        node={fwNode!}
        open={!!fwNode}
        onClose={() => setFwNode(null)}
      />

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

      {/* 生成安装脚本弹窗 */}
      <Modal
        open={scriptOpen}
        title="生成 Agent 安装脚本"
        onClose={() => setScriptOpen(false)}
        footer={
          script ? (
            <>
              <Button variant="ghost" onClick={() => setScriptOpen(false)}>关闭</Button>
              <Button onClick={() => { navigator.clipboard.writeText(script).catch(() => {}); toast('脚本已复制') }}>复制</Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setScriptOpen(false)}>取消</Button>
              <Button onClick={submitScript} disabled={scriptLoading}>
                {scriptLoading ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '生成脚本'}
              </Button>
            </>
          )
        }
      >
        {script ? (
          <div className="space-y-3">
            <p className="text-xs text-slate-500">在目标机器以 root 执行以下命令即可安装 Agent。</p>
            <pre className="narwhal-scroll max-h-96 overflow-auto rounded-lg bg-slate-50 p-4 font-mono text-xs whitespace-pre-wrap break-all">
              {script}
            </pre>
          </div>
        ) : (
          <div className="space-y-4">
            <Field label="虚拟化类型">
              <Select value={scriptVirtType} onChange={(e) => setScriptVirtType(e.target.value)}>
                <option value="incus">Incus / LXD</option>
                <option value="oci">Podman / Docker</option>
              </Select>
            </Field>
            <Field label="Web 管理密码">
              <Input value={scriptWebPassword} onChange={(e) => setScriptWebPassword(e.target.value)} placeholder="留空自动生成" />
            </Field>
            <label className="flex items-center gap-2 text-sm text-slate-600">
              <input
                type="checkbox"
                checked={scriptWithToken}
                onChange={(e) => setScriptWithToken(e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 accent-blue-600"
              />
              包含 Token（把该节点 Token 嵌入脚本，免登录配置）
            </label>
            {scriptError && <p className="text-sm text-red-600">{scriptError}</p>}
          </div>
        )}
      </Modal>

      {/* 新增套餐弹窗 */}
      <Modal
        open={pkgOpen}
        title="新增套餐"
        onClose={() => setPkgOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPkgOpen(false)}>取消</Button>
            <Button onClick={submitPackage} disabled={pkgSaving || !pkgName}>
              {pkgSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '新增'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="套餐名称">
            <Input value={pkgName} onChange={(e) => setPkgName(e.target.value)} placeholder="例如：小鸡 1C1G" />
          </Field>
          <Field label="可选镜像" hint="不选则买家可自由选择镜像">
            <div className="grid grid-cols-2 gap-2">
              {pkgImages.map((img) => (
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
              <Input type="number" min={0} value={pkgDisk} onChange={(e) => setPkgDisk(e.target.value)} />
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
            允许早期全额退款（取消机器时，开通≤1小时且流量≤1G 的实例返还全部余额）
          </label>
          {pkgError && <p className="text-sm text-red-600">{pkgError}</p>}
        </div>
      </Modal>

      {/* 新增优惠码弹窗 */}
      <Modal
        open={cpOpen}
        title="新增优惠码"
        onClose={() => setCpOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCpOpen(false)}>取消</Button>
            <Button onClick={submitCoupon} disabled={cpSaving || !cpCode || !cpPercent}>
              {cpSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '创建'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="优惠码">
            <Input value={cpCode} onChange={(e) => setCpCode(e.target.value)} placeholder="例如：NARWHAL10" />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="折扣比例 (%)">
              <Input type="number" min={0} max={100} value={cpPercent} onChange={(e) => setCpPercent(e.target.value)} />
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

      {/* 编辑优惠码弹窗 */}
      <Modal
        open={editCpOpen}
        title={`编辑优惠码 ${editCp?.code ?? ''}`}
        onClose={() => setEditCpOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setEditCpOpen(false)}>取消</Button>
            <Button onClick={submitEditCoupon} disabled={editCpSaving}>
              {editCpSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '保存'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="折扣比例 (%)">
              <Input type="number" min={0} max={100} value={editCpPercent} onChange={(e) => setEditCpPercent(e.target.value)} />
            </Field>
            <Field label="最大使用次数" hint="0 表示不限">
              <Input type="number" min={0} value={editCpMaxUses} onChange={(e) => setEditCpMaxUses(e.target.value)} />
            </Field>
          </div>
          <Field label="到期时间" hint="留空 = 永不过期；已用次数不可低于新上限">
            <Input type="datetime-local" value={editCpExpires} onChange={(e) => setEditCpExpires(e.target.value)} />
          </Field>
          <label className="flex items-center gap-2 text-sm text-slate-600">
            <input
              type="checkbox"
              checked={editCpEnabled}
              onChange={(e) => setEditCpEnabled(e.target.checked)}
              className="h-4 w-4 rounded border-slate-300 accent-blue-600"
            />
            启用
          </label>
          {editCpError && <p className="text-sm text-red-600">{editCpError}</p>}
        </div>
      </Modal>

      {/* 发放兑换码弹窗 */}
      <Modal
        open={gcOpen}
        title="发放兑换码"
        subtitle="用户可在「账单充值」页兑换，面额自动入账余额（10% 入托管余额）"
        onClose={() => setGcOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setGcOpen(false)}>取消</Button>
            <Button onClick={submitGiftCards} disabled={gcSaving || !gcAmount}>
              {gcSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '生成'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="面额 (分)">
              <Input type="number" min={1} value={gcAmount} onChange={(e) => setGcAmount(e.target.value)} placeholder="如 500 = $5" />
            </Field>
            <Field label="数量" hint="1-100">
              <Input type="number" min={1} max={100} value={gcCount} onChange={(e) => setGcCount(e.target.value)} />
            </Field>
          </div>
          <Field label="自定义编码(可选)" hint="留空自动生成 GIFT-XXXX-XXXX；批量生成时忽略">
            <Input className="font-mono" value={gcCode} onChange={(e) => setGcCode(e.target.value)} placeholder="GIFT-XXXX-XXXX" />
          </Field>
          <Field label="到期时间" hint="留空 = 永不过期">
            <Input type="datetime-local" value={gcExpires} onChange={(e) => setGcExpires(e.target.value)} />
          </Field>
          {gcError && <p className="text-sm text-red-600">{gcError}</p>}
        </div>
      </Modal>

      {/* 编辑兑换码弹窗 */}
      <Modal
        open={editGcOpen}
        title={`编辑兑换码 ${editGc?.code ?? ''}`}
        onClose={() => setEditGcOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setEditGcOpen(false)}>取消</Button>
            <Button onClick={submitEditGiftCard} disabled={editGcSaving}>
              {editGcSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '保存'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="面额 (分)">
            <Input type="number" min={1} value={editGcAmount} onChange={(e) => setEditGcAmount(e.target.value)} />
          </Field>
          <Field label="到期时间" hint="留空 = 永不过期">
            <Input type="datetime-local" value={editGcExpires} onChange={(e) => setEditGcExpires(e.target.value)} />
          </Field>
          {editGcError && <p className="text-sm text-red-600">{editGcError}</p>}
        </div>
      </Modal>

      {/* 重置用户密码弹窗 */}
      <Modal
        open={pwOpen}
        title={`重置密码 - ${pwUser?.username ?? ''}`}
        subtitle="为用户设置新的登录密码"
        onClose={() => setPwOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPwOpen(false)}>取消</Button>
            <Button onClick={submitResetPassword} disabled={pwSaving || pwValue.length < 6}>
              {pwSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '重置密码'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="新密码" hint="至少 6 位">
            <Input type="password" value={pwValue} onChange={(e) => setPwValue(e.target.value)} placeholder="输入新的登录密码" />
          </Field>
          {pwError && <p className="text-sm text-red-600">{pwError}</p>}
        </div>
      </Modal>

      {/* 调整余额弹窗 */}
      <Modal
        open={balOpen}
        title={`调整余额 - ${balUser?.username ?? ''}`}
        subtitle={`当前余额 $${((balUser?.balance_cents ?? 0) / 100).toFixed(2)}（正数加钱，负数扣钱；10% 计入托管余额）`}
        onClose={() => setBalOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setBalOpen(false)}>取消</Button>
            <Button onClick={submitAdjustBalance} disabled={balSaving || !balDelta}>
              {balSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '确认调整'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="调整金额 (USD)" hint="正数增加，负数扣除">
            <Input type="number" step="0.01" value={balDelta} onChange={(e) => setBalDelta(e.target.value)} placeholder="例如 10 或 -5.5" />
          </Field>
          <Field label="备注 (可选)">
            <Input value={balRemark} onChange={(e) => setBalRemark(e.target.value)} placeholder="例如：活动补偿" />
          </Field>
          {balError && <p className="text-sm text-red-600">{balError}</p>}
        </div>
      </Modal>

      {/* 用户详情弹窗 */}
      <Modal
        open={detailOpen}
        size="lg"
        title={`用户详情 - ${detailUser?.username ?? ''}`}
        onClose={() => setDetailOpen(false)}
        footer={
          <Button variant="ghost" onClick={() => setDetailOpen(false)}>关闭</Button>
        }
      >
        {detailLoading ? (
          <div className="flex h-32 items-center justify-center">
            <div className="h-8 w-8 animate-spin rounded-full border-2 border-black/20 border-t-black" />
          </div>
        ) : detailData ? (
          <div className="space-y-6">
            <div className="flex flex-wrap gap-2">
              <Badge color={detailData.user.role === 'admin' ? 'indigo' : 'slate'}>
                {detailData.user.role === 'admin' ? '管理员' : '普通用户'}
              </Badge>
              <Badge color={detailData.user.banned ? 'red' : 'green'}>{detailData.user.banned ? '已封禁' : '正常'}</Badge>
              <Badge color="purple">余额 ${(detailData.user.balance_cents / 100).toFixed(2)}</Badge>
              <Badge color="blue">到期 {detailData.user.expires_at ? fmtDate(detailData.user.expires_at) : '未设置'}</Badge>
            </div>

            <div>
              <h4 className="mb-2 text-sm font-bold text-slate-700">上架的宿主机 ({detailData.nodes.length})</h4>
              {detailData.nodes.length === 0 ? (
                <p className="text-sm text-slate-400">该用户没有接入宿主机</p>
              ) : (
                <div className="divide-y divide-slate-100 rounded-lg border border-slate-200">
                  {detailData.nodes.map((n) => (
                    <div key={n.id} className="flex items-center justify-between gap-3 px-3 py-2.5">
                      <div className="min-w-0">
                        <div className="text-sm font-medium text-slate-800">{n.name}</div>
                        <div className="font-mono text-xs text-slate-400">{n.region} · {n.agent_addr}</div>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge color={n.status === 'online' ? 'green' : 'red'} dot>{n.status === 'online' ? '在线' : '离线'}</Badge>
                        <span className="text-xs text-slate-400">{n.instance_count ?? 0} 实例</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div>
              <h4 className="mb-2 text-sm font-bold text-slate-700">当前实例 ({detailData.instances.length})</h4>
              {detailData.instances.length === 0 ? (
                <p className="text-sm text-slate-400">该用户没有实例</p>
              ) : (
                <div className="divide-y divide-slate-100 rounded-lg border border-slate-200">
                  {detailData.instances.map((inst) => (
                    <div key={inst.id} className="flex items-center justify-between gap-3 px-3 py-2.5">
                      <div className="min-w-0">
                        <div className="text-sm font-medium text-slate-800">{inst.display_name || inst.name}</div>
                        <div className="font-mono text-xs text-slate-400">{inst.ip} · {inst.node_name || '平台'}</div>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge color={inst.status === 'running' ? 'green' : 'slate'} dot>
                          {inst.status === 'running' ? '运行中' : inst.status}
                        </Badge>
                        <span className="text-xs text-slate-400">${(inst.price_cents / 100).toFixed(2)}/月</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          <p className="py-8 text-center text-sm text-slate-400">加载失败</p>
        )}
      </Modal>

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
