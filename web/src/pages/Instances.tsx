import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import type { ImagePreset, Instance, Package, User } from '../lib/types'
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
  cn,
  fmtBytes,
  fmtDate,
  useToast,
} from '../components/ui'
import { PlusIcon, ServerIcon } from '../components/icons'

function statusBadge(status: string) {
  if (status === 'running') return <Badge color="green" dot>运行中</Badge>
  if (status === 'stopped') return <Badge color="slate" dot>已停止</Badge>
  return <Badge color="orange" dot>{status}</Badge>
}

// 取消机器是否可全额退款：套餐开启早期全额退款 + 开通 ≤1 小时 + 已用流量 ≤1GB
function canEarlyFullRefund(inst: Instance): boolean {
  if (!inst.early_full_refund) return false
  if (Date.now() - new Date(inst.created_at).getTime() > 3600 * 1000) return false
  const used = (inst.traffic_used_up_bytes || 0) + (inst.traffic_used_down_bytes || 0)
  return used <= 1024 ** 3
}

function cancelMessage(inst: Instance): ReactNode {
  if (canEarlyFullRefund(inst)) {
    const amount = ((inst.paid_cents || inst.price_cents) / 100).toFixed(2)
    return (
      <div className="space-y-3">
        <div className="rounded-lg border-2 border-ok/40 bg-ok/10 px-3 py-2">
          <div className="flex items-baseline justify-between">
            <span className="text-xs font-medium text-slate-500">到账金额（即时到账）</span>
            <span className="font-mono text-lg font-bold text-ok">${amount}</span>
          </div>
        </div>
        <p className="leading-6">
          该实例开通未满 1 小时且已用流量未超过 1G，满足<b className="text-slate-800">早期全额退款</b>条件。取消后将全额返还 <b className="font-mono">${amount}</b> 到余额，可立即用于购买新实例或提现。
        </p>
        <p className="text-xs text-slate-400">确定取消实例「{inst.display_name || inst.name}」吗？取消后资源立即回收，无法恢复。</p>
      </div>
    )
  }
  return (
    <p className="leading-6">
      取消机器意味着释放机器且<b className="text-slate-800">不退款</b>（取消后资源立即回收，无法恢复）。确定取消实例「{inst.display_name || inst.name}」吗？
    </p>
  )
}

export default function Instances() {
  const [instances, setInstances] = useState<Instance[]>([])
  const [packages, setPackages] = useState<Package[]>([])
  const [images, setImages] = useState<ImagePreset[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [packageId, setPackageId] = useState<number>(0)
  const [image, setImage] = useState('')
  const [couponCode, setCouponCode] = useState('')
  const [name, setName] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)
  const [actionError, setActionError] = useState('')
  const [removeTarget, setRemoveTarget] = useState<Instance | null>(null)

  // 重装系统（选择镜像）
  const [rebuildTarget, setRebuildTarget] = useState<Instance | null>(null)
  const [rebuildImg, setRebuildImg] = useState('')
  const [rebuildBusy, setRebuildBusy] = useState(false)
  const [rebuildError, setRebuildError] = useState('')
  const toast = useToast()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const load = useCallback(async () => {
    try {
      const [inst, pkgs, imgs, profile] = await Promise.all([api.instances(), api.packages(), api.images(), api.profile()])
      setInstances(inst)
      setPackages(pkgs)
      setImages(imgs)
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

  // 支持 /instances?create=1 自动打开创建弹窗
  useEffect(() => {
    if (!loading && searchParams.get('create') === '1') {
      openCreate()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, searchParams])

  const openCreate = () => {
    setCreateError('')
    setPackageId(packages[0]?.id ?? 0)
    setImage('')
    setCouponCode('')
    setName('')
    setCreateOpen(true)
  }

  const selectedPkg = packages.find((p) => p.id === packageId)

  // 套餐限定镜像：非空取套餐内镜像，否则（平台套餐）给预设+自定义。
  const pkgImages = selectedPkg?.images ? selectedPkg.images.split(',').map((s) => s.trim()).filter(Boolean) : []
  const allowCustom = !selectedPkg || !selectedPkg.images

  // 本地估算折后价（后端为准，这里仅用于余额提示）。
  const price = selectedPkg?.price_cents ?? 0
  const insufficient = user !== null && user.balance_cents < price

  const submitCreate = async () => {
    setCreating(true)
    setCreateError('')
    try {
      const inst = await api.createInstance({
        package_id: packageId,
        image,
        name: name || undefined,
        coupon_code: couponCode.trim() || undefined,
      })
      setCreateOpen(false)
      toast(`实例「${inst.display_name || inst.name}」创建成功`)
      await load()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : '创建失败，请稍后重试')
    } finally {
      setCreating(false)
    }
  }

  const runAction = async (id: number, action: string) => {
    setBusyId(id)
    setActionError('')
    try {
      await api.instanceAction(id, action)
      await load()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败，请稍后重试')
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (): Promise<boolean> => {
    if (!removeTarget) return false
    setBusyId(removeTarget.id)
    setActionError('')
    try {
      await api.deleteInstance(removeTarget.id)
      toast('实例已取消')
      await load()
      return true
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '删除失败，请稍后重试')
      return false
    } finally {
      setBusyId(null)
    }
  }

  const openRebuild = (inst: Instance) => {
    setRebuildTarget(inst)
    setRebuildError('')
    setRebuildImg(inst.image || '')
  }

  const submitRebuild = async () => {
    const img = rebuildImg.trim()
    if (!img || !rebuildTarget) {
      setRebuildError('请选择或输入要重装的镜像')
      return
    }
    setRebuildBusy(true)
    setRebuildError('')
    try {
      await api.instanceAction(rebuildTarget.id, 'rebuild', img)
      toast(`已重装为 ${img}，SSH 密码保持不变，端口映射保留`)
      setRebuildTarget(null)
      await load()
    } catch (err) {
      setRebuildError(err instanceof ApiError ? err.message : '重装失败，请稍后重试')
    } finally {
      setRebuildBusy(false)
    }
  }

  const canSubmit = !creating && !!packageId && !!image.trim()

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
            实例管理 <span className="text-sm font-normal text-slate-400">Instances</span>
          </h1>
          <p className="mt-0.5 text-sm text-slate-500">共 {instances.length} 台实例</p>
        </div>
        <Button onClick={openCreate}>
          <PlusIcon className="h-4 w-4" />
          新建实例
        </Button>
      </div>

      {actionError && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">{actionError}</div>
      )}

      <Card>
        {instances.length === 0 ? (
          <Empty text="暂无实例，点击右上角「新建实例」开始" icon={<ServerIcon className="h-8 w-8" />} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                  <th className="px-5 py-3 font-medium">名称</th>
                  <th className="px-5 py-3 font-medium">IP</th>
                  <th className="px-5 py-3 font-medium">状态</th>
                  <th className="px-5 py-3 font-medium">配置</th>
                  <th className="px-5 py-3 font-medium">流量</th>
                  <th className="px-5 py-3 font-medium">到期日</th>
                  <th className="px-5 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {instances.map((inst) => {
                  const used = inst.traffic_used_up_bytes + inst.traffic_used_down_bytes
                  const total = inst.traffic_gb * 1024 ** 3
                  return (
                    <tr
                      key={inst.id}
                      className="border-b border-slate-100 odd:bg-white even:bg-slate-50/50 last:border-0 hover:bg-sky-50/60"
                    >
                      <td className="px-5 py-3">
                        <Link
                          to={`/instances/${inst.id}`}
                          className="block font-medium text-slate-900 transition-colors hover:text-brand hover:underline"
                        >
                          {inst.display_name || inst.name}
                        </Link>
                        <div className="text-xs text-slate-400">{inst.image}</div>
                      </td>
                      <td className="px-5 py-3 font-mono text-blue-600">{inst.ip}</td>
                      <td className="px-5 py-3">{statusBadge(inst.status)}</td>
                      <td className="px-5 py-3 text-slate-500">
                        {inst.cpu_cores} vCPU / {inst.memory_mb} MB / {inst.disk_mb} MB
                      </td>
                      <td className="px-5 py-3">
                        {inst.traffic_gb === 0 ? (
                          <span className="text-xs text-slate-400 font-mono">无限</span>
                        ) : (
                          <div className="w-40">
                            <Progress value={used} max={total || 1} />
                            <div className="mt-1 font-mono text-xs text-slate-400">
                              {fmtBytes(used)} / {inst.traffic_gb} GB
                            </div>
                          </div>
                        )}
                        {inst.early_full_refund && (
                          <div className="mt-1.5 w-40">
                            <div className="flex items-center justify-between">
                              <span className="text-[11px] text-slate-400">全额退款线 1G</span>
                              <span className={cn('font-mono text-[11px]', used <= 1024 ** 3 ? 'text-ok' : 'text-danger')}>
                                {used <= 1024 ** 3 ? '未超' : '已超'}
                              </span>
                            </div>
                            <div className="mt-0.5 h-1 w-full overflow-hidden rounded-full bg-slate-100">
                              <div
                                className={cn('h-full rounded-full transition-all', used <= 1024 ** 3 ? 'bg-ok' : 'bg-danger')}
                                style={{ width: `${Math.min(100, (used / 1024 ** 3) * 100)}%` }}
                              />
                            </div>
                          </div>
                        )}
                      </td>
                      <td className="px-5 py-3 font-mono text-slate-500">{inst.expires_at ? fmtDate(inst.expires_at) : '未设置'}</td>
                      <td className="px-5 py-3">
                        <div className="flex flex-wrap items-center justify-end gap-1">
                          <Link to={`/instances/${inst.id}`}>
                            <Button size="sm" variant="sky">详情</Button>
                          </Link>
                          <Link to={`/terminal/${inst.id}`}>
                            <Button size="sm" variant="secondary">终端</Button>
                          </Link>
                          {inst.status === 'running' ? (
                            <>
                              <Button size="sm" variant="outline" disabled={busyId === inst.id} onClick={() => runAction(inst.id, 'stop')}>关机</Button>
                              <Button size="sm" variant="lemon" disabled={busyId === inst.id} onClick={() => runAction(inst.id, 'restart')}>重启</Button>
                            </>
                          ) : (
                            <Button size="sm" variant="sage" disabled={busyId === inst.id} onClick={() => runAction(inst.id, 'start')}>启动</Button>
                          )}
                          <Button size="sm" variant="secondary" disabled={busyId === inst.id} onClick={() => navigate(`/market?sell=${inst.id}`)}>出售</Button>
                          <Button size="sm" variant="peach" disabled={busyId === inst.id} onClick={() => openRebuild(inst)}>重装</Button>
                          <Button size="sm" variant="danger" disabled={busyId === inst.id} onClick={() => { setActionError(''); setRemoveTarget(inst) }}>取消</Button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* 新建实例弹窗 */}
      <Modal
        open={createOpen}
        title="新建实例"
        subtitle="选择已上架的固定套餐，配置不可自定义"
        size="lg"
        onClose={() => setCreateOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button onClick={submitCreate} disabled={!canSubmit || insufficient}>
              {creating ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '立即创建'}
            </Button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
          {/* 左侧表单 */}
          <div className="space-y-4 md:col-span-2">
            <Field label="选择套餐">
              <Select
                value={packageId}
                onChange={(e) => {
                  setPackageId(Number(e.target.value))
                  setImage('')
                }}
              >
                {packages.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                    {p.node_name ? `（${p.node_name}${p.node_region ? ' · ' + p.node_region : ''}）` : '（平台）'} · ${(p.price_cents / 100).toFixed(2)}/月
                  </option>
                ))}
              </Select>
              {packages.length === 0 && <p className="mt-1 text-xs text-slate-400">暂无在售套餐</p>}
            </Field>

            <Field label="镜像" hint={pkgImages.length > 0 ? '仅限套餐提供的镜像' : '平台套餐可任选预设或自定义'}>
              {pkgImages.length > 0 ? (
                <div className="grid grid-cols-2 gap-2">
                  {pkgImages.map((ref) => (
                    <label
                      key={ref}
                      className={cn(
                        'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors',
                        image === ref
                          ? 'border-brand bg-blue-50/60 text-brand'
                          : 'border-slate-200 text-body hover:border-slate-300',
                      )}
                    >
                      <input type="radio" name="image" className="accent-brand" checked={image === ref} onChange={() => setImage(ref)} />
                      <span className="font-mono text-xs">{ref}</span>
                    </label>
                  ))}
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-2">
                    {images.map((img) => (
                      <label
                        key={img.id}
                        className={cn(
                          'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors',
                          image === img.ref
                            ? 'border-brand bg-blue-50/60 text-brand'
                            : 'border-slate-200 text-body hover:border-slate-300',
                        )}
                      >
                        <input type="radio" name="image" className="accent-brand" checked={image === img.ref} onChange={() => setImage(img.ref)} />
                        <span>{img.name}</span>
                        <span className="font-mono text-[11px] text-muted">{img.ref}</span>
                      </label>
                    ))}
                  </div>
                  <Input className="mt-2 font-mono" value={image} onChange={(e) => setImage(e.target.value)} placeholder="或输入自定义镜像 ref，如 debian/12" />
                </>
              )}
            </Field>

            <Field label="优惠码(可选)" hint="机主发布的购买折扣码，创建时自动抵扣">
              <Input value={couponCode} onChange={(e) => setCouponCode(e.target.value)} placeholder="如 NARWHAL10" />
            </Field>

            <Field label="名称(可选)">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：我的小鸡" />
            </Field>
          </div>

          {/* 右侧摘要 */}
          <div className="rounded-xl border border-slate-200 bg-slate-50 p-4">
            <div className="text-xs font-medium text-slate-500">订单摘要 Order Summary</div>
            <div className="mt-3 flex items-baseline gap-1">
              <span className="font-mono text-2xl font-semibold text-ink">${(price / 100).toFixed(2)}</span>
              <span className="text-sm text-muted">/月</span>
            </div>
            <dl className="mt-4 space-y-2 text-sm">
              <div className="flex justify-between">
                <dt className="text-slate-400">所在节点</dt>
                <dd className="font-medium text-slate-700">{selectedPkg?.node_name || '平台调度'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">地区</dt>
                <dd className="font-medium text-slate-700">{selectedPkg?.node_region || '-'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">CPU</dt>
                <dd className="font-mono text-slate-700">{selectedPkg?.cpu_cores ?? '-'} vCPU</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">内存</dt>
                <dd className="font-mono text-slate-700">{selectedPkg?.memory_mb ?? '-'} MB</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">硬盘</dt>
                <dd className="font-mono text-slate-700">{selectedPkg ? `${(selectedPkg.disk_mb / 1024).toFixed(1)} GB` : '-'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">流量</dt>
                <dd className="font-mono text-slate-700">{selectedPkg?.traffic_gb === 0 ? '无限' : `${selectedPkg?.traffic_gb ?? '-'} GB`}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">端口槽位</dt>
                <dd className="font-mono text-slate-700">{selectedPkg?.port_slots ?? '-'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-400">镜像</dt>
                <dd className="max-w-[9rem] truncate font-mono text-slate-700">{image || '-'}</dd>
              </div>
            </dl>
            <div
              className={cn(
                'mt-4 flex items-center justify-between rounded-md border border-slate-200 bg-white px-3 py-2',
                insufficient && 'border-danger/40',
              )}
            >
              <span className="text-xs text-muted">可用余额</span>
              <span className={cn('font-mono text-sm font-semibold', insufficient ? 'text-danger' : 'text-ink')}>
                ${user ? (user.balance_cents / 100).toFixed(2) : '-'}
              </span>
            </div>
            {insufficient && (
              <p className="mt-2 text-xs text-danger">
                余额不足，请先充值
                <Link to="/recharge" className="ml-1 text-brand underline">去充值</Link>
              </p>
            )}
          </div>
        </div>
        {createError && <p className="mt-3 text-sm text-red-600">{createError}</p>}
      </Modal>

      {/* 重装系统（选择镜像） */}
      <Modal
        open={!!rebuildTarget}
        title="重装系统"
        subtitle={
          rebuildTarget
            ? `实例：${rebuildTarget.display_name || rebuildTarget.name} · 选择要重装的镜像`
            : ''
        }
        onClose={() => { if (!rebuildBusy) setRebuildTarget(null) }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRebuildTarget(null)} disabled={rebuildBusy}>取消</Button>
            <Button onClick={submitRebuild} disabled={rebuildBusy || !rebuildImg.trim()}>
              {rebuildBusy ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '确认重装'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="选择镜像">
            <div className="grid grid-cols-2 gap-2">
              {images.map((img) => (
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
            <Input
              className="mt-2 font-mono"
              value={rebuildImg}
              onChange={(e) => setRebuildImg(e.target.value)}
              placeholder="或输入自定义镜像 ref，如 debian/12"
            />
          </Field>
          <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-700">
            重装将销毁容器内的所有数据。端口映射（NAT 规则）会自动保留，除非你手动删除。
          </p>
          {rebuildError && <p className="text-sm text-red-600">{rebuildError}</p>}
        </div>
      </Modal>

      {/* 取消实例确认 */}
      <ConfirmDialog
        open={!!removeTarget}
        title="取消机器"
        confirmText="确认取消"
        message={removeTarget ? cancelMessage(removeTarget) : ''}
        onConfirm={remove}
        onClose={() => setRemoveTarget(null)}
      />
    </div>
  )
}
