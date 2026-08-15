import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import type { Instance, InstanceStats, PortMapping } from '../lib/types'
import { HistorySparkline, useHistory } from '../components/charts'
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
  Progress,
  Select,
  Separator,
  cn,
  fmtBytes,
  fmtDate,
  useToast,
} from '../components/ui'
import {
  ArrowLeftIcon,
  CoinsIcon,
  CreditCardIcon,
  EyeIcon,
  EyeOffIcon,
  KeyIcon,
  PowerIcon,
  RefreshIcon,
  RotateCwIcon,
  TerminalIcon,
  TrashIcon,
} from '../components/icons'

// 取消机器是否可全额退款：套餐开启早期全额退款 + 开通 ≤1 小时 + 已用流量 ≤1GB
function canEarlyFullRefund(inst: Instance): boolean {
  if (!inst.early_full_refund) return false
  if (Date.now() - new Date(inst.created_at).getTime() > 3600 * 1000) return false
  const used = (inst.traffic_used_up_bytes || 0) + (inst.traffic_used_down_bytes || 0)
  return used <= 1024 ** 3
}

function cancelMessage(inst: Instance): string {
  if (canEarlyFullRefund(inst)) {
    return `该实例开通未满 1 小时且流量未超过 1G，取消将返还全部余额 $${((inst.paid_cents || inst.price_cents) / 100).toFixed(2)}。确定取消实例「${inst.display_name || inst.name}」吗？`
  }
  return `取消机器意味着释放机器且不退款（取消后资源立即回收，无法恢复）。确定取消实例「${inst.display_name || inst.name}」吗？`
}

export default function InstanceDetail() {
  const { id } = useParams<{ id: string }>()
  const instanceId = Number(id)
  const navigate = useNavigate()
  const toast = useToast()

  const [instance, setInstance] = useState<Instance | null>(null)
  const [ports, setPorts] = useState<PortMapping[]>([])
  const [stats, setStats] = useState<InstanceStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState('')
  const [containerPort, setContainerPort] = useState('')
  const [protocol, setProtocol] = useState('tcp')
  const [hostPort, setHostPort] = useState('')
  const [showPw, setShowPw] = useState(false)

  // 删除确认
  const [confirmRemoveOpen, setConfirmRemoveOpen] = useState(false)
  const [portToRemove, setPortToRemove] = useState<PortMapping | null>(null)

  // 修改密码
  const [pwOpen, setPwOpen] = useState(false)
  const [pwSaving, setPwSaving] = useState(false)
  const [pwError, setPwError] = useState('')
  const [newPassword, setNewPassword] = useState('')

  // 重装系统（选择镜像）
  const [rebuildOpen, setRebuildOpen] = useState(false)
  const [rebuildImg, setRebuildImg] = useState('')
  const [rebuildPresets, setRebuildPresets] = useState<Array<{ id: string; name: string; ref: string }>>([])
  const [rebuildBusy, setRebuildBusy] = useState(false)
  const [rebuildError, setRebuildError] = useState('')

  const loadInstance = useCallback(async () => {
    try {
      const inst = await api.instance(instanceId)
      setInstance(inst)
      const mappings = await api.ports(instanceId)
      setPorts(mappings)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载实例失败')
    } finally {
      setLoading(false)
    }
  }, [instanceId])

  // 实时图表采样历史
  const prevRx = useRef(0)
  const prevTx = useRef(0)
  const lastSample = useRef(0)
  const { push } = useHistory(20)
  const [cpuHist, setCpuHist] = useState<number[]>([])
  const [memHist, setMemHist] = useState<number[]>([])
  const [netInHist, setNetInHist] = useState<number[]>([])
  const [netOutHist, setNetOutHist] = useState<number[]>([])

  const loadStats = useCallback(async () => {
    try {
      const s = await api.stats(instanceId)
      setStats(s)
      setCpuHist((h) => push(h, s.cpu_percent))
      setMemHist((h) => push(h, s.memory_used_mb))
      const now = Date.now()
      if (prevRx.current > 0 && now - lastSample.current >= 1000) {
        const dt = (now - lastSample.current) / 1000
        if (s.rx_bytes >= prevRx.current) setNetInHist((h) => push(h, (s.rx_bytes - prevRx.current) / dt))
        if (s.tx_bytes >= prevTx.current) setNetOutHist((h) => push(h, (s.tx_bytes - prevTx.current) / dt))
      }
      prevRx.current = s.rx_bytes
      prevTx.current = s.tx_bytes
      lastSample.current = now
    } catch {
      // 统计接口失败时静默
    }
  }, [instanceId, push])

  useEffect(() => {
    loadInstance()
    loadStats()
    // 30s 轮询一次实时统计：降低采集频率，避免持续请求对宿主机造成压力
    const t = setInterval(loadStats, 30000)
    return () => clearInterval(t)
  }, [loadInstance, loadStats])

  const runAction = async (action: string) => {
    setBusy(true)
    setActionError('')
    try {
      await api.instanceAction(instanceId, action)
      await loadInstance()
      toast('操作已执行')
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (): Promise<boolean> => {
    setBusy(true)
    setActionError('')
    try {
      await api.deleteInstance(instanceId)
      toast('实例已注销')
      navigate('/instances')
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '注销失败')
      return false
    } finally {
      setBusy(false)
    }
  }

  const openAdd = () => {
    setAddError('')
    setContainerPort('')
    setProtocol('tcp')
    setHostPort('')
    setAddOpen(true)
  }

  const submitAdd = async () => {
    setAdding(true)
    setAddError('')
    try {
      await api.addPort(instanceId, {
        container_port: Number(containerPort),
        protocol,
        host_port: hostPort ? Number(hostPort) : undefined,
      })
      setAddOpen(false)
      toast('端口规则已添加')
      await loadInstance()
    } catch (err) {
      setAddError(err instanceof ApiError ? err.message : '添加规则失败')
    } finally {
      setAdding(false)
    }
  }

  const removePort = async (): Promise<boolean> => {
    if (!portToRemove) return false
    setActionError('')
    try {
      await api.deletePort(portToRemove.id)
      toast('端口规则已删除')
      await loadInstance()
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '删除映射失败')
      return false
    }
  }

  const genPassword = () => {
    setNewPassword(
      Array.from(crypto.getRandomValues(new Uint8Array(8)))
        .map((b) => b.toString(16).padStart(2, '0'))
        .join(''),
    )
  }

  const submitChangePassword = async () => {
    setPwSaving(true)
    setPwError('')
    try {
      await api.changePassword(instanceId, newPassword)
      setPwOpen(false)
      toast('SSH 密码已更新')
      await loadInstance()
    } catch (err) {
      setPwError(err instanceof ApiError ? err.message : '修改密码失败')
    } finally {
      setPwSaving(false)
    }
  }

  const openRebuild = async () => {
    setRebuildError('')
    setRebuildImg(instance?.image ?? '')
    try {
      const imgs = await api.images()
      setRebuildPresets(imgs)
    } catch {
      setRebuildPresets([])
    }
    setRebuildOpen(true)
  }

  const submitRebuild = async () => {
    const img = rebuildImg.trim()
    if (!img) {
      setRebuildError('请选择或输入要重装的镜像')
      return
    }
    setRebuildBusy(true)
    setRebuildError('')
    try {
      await api.instanceAction(instanceId, 'rebuild', img)
      setRebuildOpen(false)
      toast(`已重装为 ${img}，SSH 密码保持不变，端口映射保留`)
      await loadInstance()
    } catch (err) {
      setRebuildError(err instanceof ApiError ? err.message : '重装失败，请稍后重试')
    } finally {
      setRebuildBusy(false)
    }
  }

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-blue-600" />
      </div>
    )
  }

  if (error || !instance) {
    return <Card className="p-8 text-center text-sm text-red-600">{error || '实例不存在'}</Card>
  }

  const sshMapping = ports.find((p) => p.container_port === 22)
  const sshCommand = sshMapping ? `ssh root@${instance.node_host_ip} -p ${sshMapping.host_port}` : null
  const trafficUsed = instance.traffic_used_up_bytes + instance.traffic_used_down_bytes
  const trafficTotal = instance.traffic_gb * 1024 ** 3
  const unlimited = instance.traffic_gb === 0
  const memPct = stats ? (stats.memory_limit_mb > 0 ? stats.memory_used_mb / stats.memory_limit_mb : 0) : 0

  const spec = [
    { label: 'IPv4', value: instance.ip },
    { label: '宿主机', value: instance.node_host_ip },
    { label: '所在节点', value: `${instance.node_name} (${instance.node_region})` },
    { label: '磁盘', value: fmtBytes(instance.disk_mb * 1024 * 1024) },
    { label: '流量', value: unlimited ? '无限' : `${instance.traffic_gb} GB` },
    { label: '到期时间', value: instance.expires_at ? fmtDate(instance.expires_at) : '未设置' },
  ]

  return (
    <div className="space-y-6">
      {/* 面包屑 + 标题 */}
      <div>
        <nav className="text-xs text-slate-400">
          <Link to="/" className="hover:text-blue-600">控制面板</Link>
          <span className="mx-1.5">/</span>
          <Link to="/instances" className="hover:text-blue-600">实例管理</Link>
          <span className="mx-1.5">/</span>
          <span className="text-slate-600">{instance.display_name || instance.name}</span>
        </nav>
        <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Link to="/instances" className="mr-1">
              <Button size="sm" variant="outline" title="返回实例管理">
                <ArrowLeftIcon className="h-3.5 w-3.5" />
                返回实例管理
              </Button>
            </Link>
            <h1 className="text-lg font-semibold text-slate-900">{instance.display_name || instance.name}</h1>
            {instance.status === 'running' ? (
              <Badge color="green" dot>运行中</Badge>
            ) : instance.status === 'stopped' ? (
              <Badge color="slate" dot>已停止</Badge>
            ) : (
              <Badge color="orange" dot>{instance.status}</Badge>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={loadStats}>
              <RefreshIcon className="h-3.5 w-3.5" />
              刷新
            </Button>
            <Link to={`/terminal/${instance.id}`}>
              <Button size="sm">
                <TerminalIcon className="h-3.5 w-3.5" />
                打开终端
              </Button>
            </Link>
          </div>
        </div>
      </div>

      {actionError && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">{actionError}</div>
      )}

      {/* 资源卡片 */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card>
          <CardContent className="p-5">
            <div className="text-sm text-slate-500">CPU 使用率</div>
            <div className="mt-3">
              <Progress value={stats?.cpu_percent ?? 0} max={100} label={`${(stats?.cpu_percent ?? 0).toFixed(1)}%`} />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-5">
            <div className="text-sm text-slate-500">内存</div>
            <div className="mt-3">
              <Progress
                value={stats?.memory_used_mb ?? 0}
                max={stats?.memory_limit_mb || 1}
                label={`${stats ? stats.memory_used_mb.toFixed(0) : 0} MB / ${stats ? stats.memory_limit_mb.toFixed(0) : instance.memory_mb} MB`}
              />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-5">
            <div className="text-sm text-slate-500">月流量</div>
            <div className="mt-3">
              <Progress
                value={unlimited ? 0 : trafficUsed}
                max={unlimited ? 1 : trafficTotal}
                label={unlimited ? '无限流量' : `${fmtBytes(trafficUsed)} / ${instance.traffic_gb} GB`}
              />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-5">
            <div className="text-sm text-slate-500">网络速率</div>
            <div className="mt-3 grid grid-cols-2 gap-2 text-center">
              <div className="rounded-lg bg-slate-50 py-2">
                <div className="text-xs text-slate-400">下行</div>
                <div className="font-mono text-sm font-semibold text-slate-800">{fmtBytes(netInHist[netInHist.length - 1] ?? 0)}/s</div>
              </div>
              <div className="rounded-lg bg-slate-50 py-2">
                <div className="text-xs text-slate-400">上行</div>
                <div className="font-mono text-sm font-semibold text-slate-800">{fmtBytes(netOutHist[netOutHist.length - 1] ?? 0)}/s</div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 实时资源曲线 */}
      <Card>
        <CardHeader title="实时资源曲线" subtitle="最近 10 分钟采样（每 30 秒刷新）" />
        <CardContent>
          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 xl:grid-cols-4">
            <HistorySparkline label="CPU 使用率" value={`${(stats?.cpu_percent ?? 0).toFixed(1)}%`} data={cpuHist} max={100} color="#4F46E5" />
            <HistorySparkline
              label="内存"
              value={`${stats?.memory_used_mb ?? 0} / ${stats?.memory_limit_mb ?? instance.memory_mb} MB`}
              data={memHist}
              color="#22C55E"
            />
            <HistorySparkline label="下行速率" value={`${fmtBytes(netInHist[netInHist.length - 1] ?? 0)}/s`} data={netInHist} color="#0EA5E9" />
            <HistorySparkline label="上行速率" value={`${fmtBytes(netOutHist[netOutHist.length - 1] ?? 0)}/s`} data={netOutHist} color="#F59E0B" />
          </div>
        </CardContent>
      </Card>

      {/* 规格信息 */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <Card>
          <CardHeader title="实例信息" subtitle="Instance Specs" />
          <CardContent>
            <dl className="space-y-2.5 text-sm">
              {spec.map((s) => (
                <div key={s.label} className="flex items-center justify-between gap-3">
                  <dt className="text-slate-400">{s.label}</dt>
                  <dd className="font-mono text-slate-700">{s.value}</dd>
                </div>
              ))}
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-400">配置</dt>
                <dd className="font-mono text-slate-700">
                  {instance.cpu_cores}C / {instance.memory_mb}M
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-400">系统</dt>
                <dd className="text-slate-700">{instance.os || instance.image}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-slate-400">自动续费</dt>
                <dd className="text-slate-700">{instance.auto_renew ? '已开启' : '已关闭'}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        {/* 登录凭据 */}
        <Card>
          <CardHeader title="登录凭据" subtitle="Credentials" />
          <CardContent className="space-y-3">
            <div>
              <div className="mb-1.5 text-xs font-medium text-slate-400">SSH 命令</div>
              {sshCommand ? (
                <div className="flex items-center gap-2">
                  <code className="flex-1 rounded-md bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800 break-all">
                    {sshCommand}
                  </code>
                  <CopyButton value={sshCommand} />
                </div>
              ) : (
                <p className="text-xs text-slate-400">未找到 SSH(22) 端口映射</p>
              )}
            </div>
            <Separator />
            <div>
              <div className="mb-1.5 text-xs font-medium text-slate-400">root 密码</div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-md bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800">
                  {instance.ssh_password ? (showPw ? instance.ssh_password : '••••••••') : '尚未设置'}
                </code>
                {instance.ssh_password && (
                  <button
                    onClick={() => setShowPw(!showPw)}
                    className="inline-flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2.5 py-1.5 text-xs font-bold text-ink shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none"
                  >
                    {showPw ? <EyeOffIcon className="h-3.5 w-3.5" /> : <EyeIcon className="h-3.5 w-3.5" />}
                    {showPw ? '隐藏' : '显示'}
                  </button>
                )}
                {instance.ssh_password && <CopyButton value={instance.ssh_password} />}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* 操作面板 */}
        <Card>
          <CardHeader title="操作面板" subtitle="Operations" />
          <CardContent className="space-y-2">
            <div className="grid grid-cols-2 gap-2">
              <Button size="sm" variant="outline" disabled={busy || instance.status !== 'running'} onClick={() => runAction('stop')}>
                <PowerIcon className="h-3.5 w-3.5" />
                关机
              </Button>
              <Button size="sm" variant="outline" disabled={busy || instance.status !== 'running'} onClick={() => runAction('restart')}>
                <RotateCwIcon className="h-3.5 w-3.5" />
                重启
              </Button>
              <Button size="sm" variant="peach" disabled={busy} onClick={() => { setActionError(''); openRebuild() }}>
                <RefreshIcon className="h-3.5 w-3.5" />
                重装系统
              </Button>
              <Button size="sm" variant="secondary" disabled={busy} onClick={() => { setPwError(''); setNewPassword(''); setPwOpen(true) }}>
                <KeyIcon className="h-3.5 w-3.5" />
                修改密码
              </Button>
              <Button size="sm" variant="lemon" disabled={busy} onClick={() => navigate(`/market?sell=${instance.id}`)}>
                <CoinsIcon className="h-3.5 w-3.5" />
                上架出售
              </Button>
              <Button size="sm" variant="lemon" onClick={() => navigate('/recharge')}>
                <CreditCardIcon className="h-3.5 w-3.5" />
                充值
              </Button>
              <Button size="sm" variant="danger" disabled={busy} onClick={() => { setActionError(''); setConfirmRemoveOpen(true) }}>
                <TrashIcon className="h-3.5 w-3.5" />
                取消机器
              </Button>
            </div>
            <p className="pt-1 text-xs text-slate-400">实例价格 ${(instance.price_cents / 100).toFixed(2)}/月，创建于 {fmtDate(instance.created_at)}</p>
          </CardContent>
        </Card>
      </div>

      {/* 端口转发 */}
      <Card>
        <CardHeader
          title={
            <span>
              端口转发 <span className="text-xs font-normal text-slate-400">Port Forwarding</span>
            </span>
          }
          subtitle={`已用 ${ports.length} / ${instance.port_slots} 个端口槽位`}
          right={
            <Button size="sm" onClick={openAdd}>添加规则</Button>
          }
        />
        {ports.length === 0 ? (
          <Empty text="暂无端口规则" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                  <th className="px-5 py-3 font-medium">协议</th>
                  <th className="px-5 py-3 font-medium">外部端口</th>
                  <th className="px-5 py-3 font-medium">内部地址</th>
                  <th className="px-5 py-3 font-medium">备注</th>
                  <th className="px-5 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {ports.map((p) => (
                  <tr key={p.id} className="border-b border-slate-50 last:border-0 hover:bg-slate-50/60">
                    <td className="px-5 py-3">
                      <Badge color={p.protocol === 'tcp' ? 'blue' : 'orange'}>{p.protocol.toUpperCase()}</Badge>
                    </td>
                    <td className="px-5 py-3 font-mono text-blue-600">{p.host_port}</td>
                    <td className="px-5 py-3 font-mono text-slate-600">{p.container_ip}:{p.container_port}</td>
                    <td className="px-5 py-3 text-slate-400">-</td>
                    <td className="px-5 py-3 text-right">
                      <Button size="sm" variant="ghost" className="text-red-600 hover:bg-red-50" onClick={() => { setActionError(''); setPortToRemove(p) }}>删除</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* 添加端口规则 */}
      <Modal
        open={addOpen}
        title="添加端口规则"
        onClose={() => setAddOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAddOpen(false)}>取消</Button>
            <Button onClick={submitAdd} disabled={adding || !containerPort}>
              {adding ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '添加'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="协议">
            <Select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </Select>
          </Field>
          <Field label="容器端口">
            <Input type="number" min={1} max={65535} value={containerPort} onChange={(e) => setContainerPort(e.target.value)} placeholder="例如：22" />
          </Field>
          <Field label="外部端口(可选)" hint="留空自动分配">
            <Input type="number" min={1} max={65535} value={hostPort} onChange={(e) => setHostPort(e.target.value)} placeholder="自动分配" />
          </Field>
          {addError && <p className="text-sm text-red-600">{addError}</p>}
        </div>
      </Modal>

      {/* 修改 SSH 密码 */}
      <Modal
        open={pwOpen}
        title="修改 SSH 密码"
        onClose={() => setPwOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPwOpen(false)}>取消</Button>
            <Button onClick={submitChangePassword} disabled={pwSaving || newPassword.length < 6}>
              {pwSaving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '保存'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="新密码">
            <Input value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="至少 6 位" />
          </Field>
          <Button size="sm" variant="ghost" onClick={genPassword}>随机生成</Button>
          {pwError && <p className="text-sm text-red-600">{pwError}</p>}
        </div>
      </Modal>

      {/* 重装系统（选择镜像） */}
      <Modal
        open={rebuildOpen}
        title="重装系统"
        subtitle="选择要重装的镜像，SSH 密码与端口映射保持不变"
        onClose={() => setRebuildOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRebuildOpen(false)} disabled={rebuildBusy}>取消</Button>
            <Button onClick={submitRebuild} disabled={rebuildBusy || !rebuildImg.trim()}>
              {rebuildBusy ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '确认重装'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="选择镜像">
            <div className="grid grid-cols-2 gap-2">
              {rebuildPresets.map((img) => (
                <label
                  key={img.id}
                  className={cn(
                    'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors',
                    rebuildImg === img.ref
                      ? 'border-brand bg-blue-50/60 text-brand'
                      : 'border-slate-200 text-body hover:border-slate-300',
                  )}
                >
                  <input type="radio" name="rebuild-image" className="accent-brand" checked={rebuildImg === img.ref} onChange={() => setRebuildImg(img.ref)} />
                  <span>{img.name}</span>
                  <span className="font-mono text-[11px] text-muted">{img.ref}</span>
                </label>
              ))}
            </div>
            <Input className="mt-2 font-mono" value={rebuildImg} onChange={(e) => setRebuildImg(e.target.value)} placeholder="或输入自定义镜像 ref，如 debian/12" />
          </Field>
          <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-700">
            重装将销毁容器内的所有数据。端口映射（NAT 规则）会自动保留，除非你手动删除。
          </p>
          {rebuildError && <p className="text-sm text-red-600">{rebuildError}</p>}
        </div>
      </Modal>

      {/* 取消机器确认 */}
      <ConfirmDialog
        open={confirmRemoveOpen}
        title="取消机器"
        confirmText="确认取消"
        message={cancelMessage(instance)}
        onConfirm={remove}
        onClose={() => setConfirmRemoveOpen(false)}
      />

      {/* 删除端口映射确认 */}
      <ConfirmDialog
        open={!!portToRemove}
        title="删除端口映射"
        message={
          portToRemove
            ? `确定删除端口映射 ${portToRemove.host_port} -> ${portToRemove.container_ip}:${portToRemove.container_port} 吗？`
            : ''
        }
        onConfirm={removePort}
        onClose={() => setPortToRemove(null)}
      />
    </div>
  )
}
