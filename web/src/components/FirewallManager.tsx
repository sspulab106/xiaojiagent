import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../lib/api'
import type { FirewallRule, Node } from '../lib/types'
import { Badge, Button, Field, Input, Modal, Select, cn, useToast } from './ui'
import { ShieldIcon, TrashIcon, PlusIcon } from './icons'

const PROTOCOLS = ['tcp', 'udp', 'http', 'tls', 'socks', 'ssh', 'fet', 'wireguard', 'openvpn', 'quic', 'all']

interface Props {
  node: Node | null
  open: boolean
  onClose: () => void
  onChanged?: () => void
}

function ruleSummary(r: FirewallRule): string {
  const target =
    r.ip_type === 'geoip' ? (r.countries ?? []).join(',') : r.ip_type === 'cidr' ? r.ip || '' : '任意'
  const port = r.port_start === r.port_end ? String(r.port_start) : `${r.port_start}-${r.port_end}`
  return `优先级 ${r.priority} · ${target || '全部来源'}`
}

export default function FirewallManager({ node, open, onClose, onChanged }: Props) {
  const toast = useToast()
  const [rules, setRules] = useState<FirewallRule[]>([])
  const [installed, setInstalled] = useState(false)
  const [iface, setIface] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 新增规则表单
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState('')
  const [direction, setDirection] = useState('in')
  const [action, setAction] = useState('block')
  const [protocol, setProtocol] = useState('tcp')
  const [portStart, setPortStart] = useState('')
  const [portEnd, setPortEnd] = useState('')
  const [priority, setPriority] = useState('0')
  const [enabled, setEnabled] = useState(true)
  const [ipType, setIpType] = useState('any')
  const [cidr, setCidr] = useState('')
  const [countries, setCountries] = useState('')
  // 二次确认删除：首次点击变「确认删除?」，3 秒后自动复位
  const [confirmId, setConfirmId] = useState<number | null>(null)

  const load = useCallback(async () => {
    if (!open || !node) return
    setLoading(true)
    setError('')
    try {
      const fw = await api.nodeFirewall(node.id)
      setRules(fw.rules ?? [])
      setInstalled(!!fw.status?.iface)
      setIface(fw.status?.iface ?? '')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载防火墙失败')
      setRules([])
      setInstalled(false)
    } finally {
      setLoading(false)
    }
  }, [open, node?.id])

  useEffect(() => {
    if (open) {
      setShowForm(false)
      setFormError('')
      load()
    }
  }, [open, load])

  const resetForm = () => {
    setDirection('in')
    setAction('block')
    setProtocol('tcp')
    setPortStart('')
    setPortEnd('')
    setPriority('0')
    setEnabled(true)
    setIpType('any')
    setCidr('')
    setCountries('')
    setFormError('')
  }

  const submit = async () => {
    if (!node) return
    setSaving(true)
    setFormError('')
    try {
      const body: Parameters<typeof api.createFirewallRule>[1] = {
        direction,
        action,
        protocol,
        priority: Number(priority) || 0,
        enabled,
        port_start: portStart.trim() !== '' ? Number(portStart) : 0,
      }
      if (portEnd.trim() !== '') body.port_end = Number(portEnd)
      body.ip_type = ipType
      if (ipType === 'cidr') body.ip = cidr.trim()
      if (ipType === 'geoip') body.countries = countries.split(',').map((s) => s.trim()).filter(Boolean)
      await api.createFirewallRule(node.id, body)
      toast('规则已添加')
      setShowForm(false)
      resetForm()
      await load()
      onChanged?.()
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : '添加规则失败')
    } finally {
      setSaving(false)
    }
  }

  const removeRule = async (r: FirewallRule) => {
    if (!node) return
    if (confirmId !== r.id) {
      setConfirmId(r.id)
      setTimeout(() => setConfirmId((cur) => (cur === r.id ? null : cur)), 3000)
      return
    }
    setConfirmId(null)
    try {
      await api.deleteFirewallRule(node.id, r.id)
      toast('规则已删除')
      await load()
      onChanged?.()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '删除规则失败')
    }
  }

  return (
    <Modal
      open={open}
      size="lg"
      title={
        <span className="inline-flex items-center gap-2">
          <ShieldIcon className="h-4 w-4" />
          防火墙规则 - {node?.name ?? ''}
        </span>
      }
      subtitle={
        installed
          ? `rfw eBPF 防火墙运行中 · 网卡 ${iface} · 共 ${rules.length} 条规则`
          : 'rfw 未运行或未安装（安装脚本会部署 rfw，或检查节点 Agent 日志）'
      }
      onClose={onClose}
      footer={
        <div className="flex items-center justify-between w-full">
          <Badge color={installed ? 'green' : 'red'} dot>{installed ? '已启用' : '未就绪'}</Badge>
          <Button variant="ghost" onClick={onClose}>关闭</Button>
        </div>
      }
    >
      <div className="space-y-5">
        {/* 添加规则 */}
        <div className="rounded-2xl border-2 border-black bg-lavender/20 p-4 shadow-hard-sm">
          <div className="flex items-center justify-between">
            <h4 className="text-sm font-extrabold text-black">添加规则</h4>
            <Button
              size="sm"
              variant={showForm ? 'outline' : 'sage'}
              onClick={() => { setShowForm(!showForm); if (!showForm) resetForm() }}
            >
              {showForm ? '收起' : <><PlusIcon className="h-3.5 w-3.5" />新规则</>}
            </Button>
          </div>

          {showForm && (
            <div className="mt-4 space-y-4">
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <Field label="方向">
                  <Select value={direction} onChange={(e) => setDirection(e.target.value)}>
                    <option value="in">入站 in</option>
                    <option value="out">出站 out</option>
                  </Select>
                </Field>
                <Field label="动作">
                  <Select value={action} onChange={(e) => setAction(e.target.value)}>
                    <option value="block">拦截 block</option>
                    <option value="pass">放行 pass</option>
                  </Select>
                </Field>
                <Field label="协议">
                  <Select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
                    {PROTOCOLS.map((p) => <option key={p} value={p}>{p}</option>)}
                  </Select>
                </Field>
                <Field label="优先级">
                  <Input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} placeholder="0" />
                </Field>
                <Field label="起始端口" hint="0 = 全部端口">
                  <Input type="number" min={0} max={65535} value={portStart} onChange={(e) => setPortStart(e.target.value)} placeholder="如 25 / 0" />
                </Field>
                <Field label="结束端口(可选)">
                  <Input type="number" min={0} max={65535} value={portEnd} onChange={(e) => setPortEnd(e.target.value)} placeholder="留空=单端口" />
                </Field>
              </div>

              <Field label="来源 IP 类型">
                <Select value={ipType} onChange={(e) => setIpType(e.target.value)}>
                  <option value="any">任意来源</option>
                  <option value="cidr">CIDR 网段</option>
                  <option value="geoip">GeoIP 国家</option>
                </Select>
              </Field>
              {ipType === 'cidr' && (
                <Field label="CIDR 网段" hint="例如 1.2.3.0/24">
                  <Input value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="1.2.3.0/24" />
                </Field>
              )}
              {ipType === 'geoip' && (
                <Field label="国家代码" hint="逗号分隔，如 CN, US">
                  <Input value={countries} onChange={(e) => setCountries(e.target.value)} placeholder="CN, US" />
                </Field>
              )}

              <label className="flex items-center gap-2 text-sm font-medium text-black">
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                  className="h-4 w-4 rounded accent-[#4338CA]"
                />
                立即启用
              </label>
              {formError && <p className="text-sm font-medium text-red-600">{formError}</p>}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={() => setShowForm(false)}>取消</Button>
                <Button size="sm" onClick={submit} disabled={saving}>
                  {saving ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '添加规则'}
                </Button>
              </div>
            </div>
          )}
        </div>

        {/* 规则列表 */}
        {loading ? (
          <div className="flex h-24 items-center justify-center">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-black/20 border-t-black" />
          </div>
        ) : error ? (
          <div className="rounded-xl border-2 border-black bg-peach/30 p-4 text-sm font-medium text-black">
            {error}
            <Button size="sm" variant="outline" className="ml-3" onClick={load}>重试</Button>
          </div>
        ) : rules.length === 0 ? (
          <div className="rounded-xl border-2 border-dashed border-black/30 p-8 text-center text-sm text-slate-500">
            {installed ? '暂无规则 —— 所有流量按系统默认放行' : 'rfw 未就绪，无法读取规则'}
          </div>
        ) : (
          <div className="space-y-2">
            {rules.map((r) => (
              <div
                key={r.id}
                className={cn(
                  'flex items-center gap-3 rounded-xl border-2 border-black px-3 py-2.5 shadow-hard-sm transition-transform hover:-translate-y-0.5',
                  r.action === 'block' ? 'bg-rose-200/70' : r.direction === 'in' ? 'bg-mint/60' : 'bg-sky-200/60',
                )}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge color={r.action === 'block' ? 'red' : 'green'}>{r.action === 'block' ? '拦截' : '放行'}</Badge>
                    <Badge color={r.direction === 'in' ? 'indigo' : 'slate'}>{r.direction === 'in' ? '入站' : '出站'}</Badge>
                    <span className="font-mono text-sm font-bold text-black">{r.protocol}</span>
                    <span className="font-mono text-xs text-slate-700">
                      {r.port_start === r.port_end ? `:${r.port_start}` : `:${r.port_start}-${r.port_end}`}
                    </span>
                    <span className="font-mono text-xs text-slate-500">
                      {r.ip_type === 'geoip' ? `geoip:${(r.countries ?? []).join(',')}` : r.ip_type === 'cidr' ? r.ip : 'any'}
                    </span>
                    {!r.enabled && <Badge color="slate">已停用</Badge>}
                  </div>
                  <div className="mt-0.5 font-mono text-[11px] text-slate-500">{ruleSummary(r)}</div>
                </div>
                <Button
                  size="sm"
                  variant={confirmId === r.id ? 'danger' : 'ghost'}
                  className={confirmId === r.id ? 'text-white' : 'text-red-600'}
                  onClick={() => removeRule(r)}
                  title="删除规则"
                >
                  {confirmId === r.id ? '确认删除?' : <TrashIcon className="h-4 w-4" />}
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </Modal>
  )
}
