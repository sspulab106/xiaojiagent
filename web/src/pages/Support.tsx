import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../lib/api'
import type { Instance, Ticket, User } from '../lib/types'
import {
  Badge,
  Button,
  Card,
  Empty,
  Field,
  Input,
  Modal,
  Select,
  Textarea,
  cn,
  fmtDate,
  fmtTime,
  useToast,
} from '../components/ui'
import { ArrowLeftIcon, CheckIcon, HeadphonesIcon, SendIcon } from '../components/icons'

// 角色判定：管理员或宿主机所有者（拥有工单所属节点）→ 属于「处理侧」。
// 通过 /tickets 的可见性推断：能作为处理侧看到他人工单的用户（管理员/机主）。
function isNodeSide(user: User | null, ticket: Ticket): boolean {
  if (!user) return false
  if (user.role === 'admin') return true
  // 机主能看到自己的工单，也能看到路由到自己节点的他人工单；列表中
  // 他人发起的工单即处理侧。这里以 ticket.user_id 是否为自己判断。
  return ticket.user_id !== user.id
}

function statusBadge(t: Ticket) {
  if (t.status === 'resolved') return <Badge color="green">已解决</Badge>
  return <Badge color="orange" dot>处理中</Badge>
}

export default function Support() {
  const toast = useToast()
  const [user, setUser] = useState<User | null>(null)
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [myInstances, setMyInstances] = useState<Instance[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionError, setActionError] = useState('')

  // 新建工单弹窗
  const [createOpen, setCreateOpen] = useState(false)
  const [instanceId, setInstanceId] = useState(0)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')

  // 工单详情弹窗
  const [detail, setDetail] = useState<Ticket | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [replyText, setReplyText] = useState('')
  const [replying, setReplying] = useState(false)

  const load = useCallback(async () => {
    try {
      const [tks, profile, insts] = await Promise.all([api.tickets(), api.profile(), api.instances()])
      setTickets(tks)
      setUser(profile)
      setMyInstances(insts)
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

  const isOwnerSide = user?.role === 'admin' || (user ? tickets.some((t) => t.user_id !== user.id) : false)

  const openCreate = () => {
    setCreateError('')
    setInstanceId(myInstances[0]?.id ?? 0)
    setTitle('')
    setContent('')
    setCreateOpen(true)
  }

  const submitCreate = async () => {
    if (!instanceId || !title.trim() || !content.trim()) {
      setCreateError('请选择实例并填写标题与问题描述')
      return
    }
    setCreating(true)
    setCreateError('')
    try {
      await api.createTicket({ instance_id: instanceId, title: title.trim(), content: content.trim() })
      setCreateOpen(false)
      toast('工单已提交，将送达宿主机技术支持')
      await load()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : '提交失败，请稍后重试')
    } finally {
      setCreating(false)
    }
  }

  const openDetail = async (t: Ticket) => {
    setActionError('')
    setDetail(t)
    setDetailLoading(true)
    setReplyText('')
    try {
      const full = await api.ticket(t.id)
      setDetail(full)
      // 刷新未读状态
      await load()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '加载工单详情失败')
    } finally {
      setDetailLoading(false)
    }
  }

  const submitReply = async () => {
    if (!detail || !replyText.trim()) return
    setReplying(true)
    setActionError('')
    try {
      await api.replyTicket(detail.id, replyText.trim())
      setReplyText('')
      const full = await api.ticket(detail.id)
      setDetail(full)
      await load()
      toast('回复已发送')
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '回复失败')
    } finally {
      setReplying(false)
    }
  }

  const resolve = async (t: Ticket) => {
    setActionError('')
    try {
      const full = await api.resolveTicket(t.id)
      setDetail(full)
      toast('工单已标记为已解决')
      await load()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败')
    }
  }

  const reopen = async (t: Ticket) => {
    setActionError('')
    try {
      const full = await api.reopenTicket(t.id)
      setDetail(full)
      toast('工单已重新打开')
      await load()
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作失败')
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

  const myTickets = user ? tickets.filter((t) => t.user_id === user.id) : []
  const nodeTickets = user ? tickets.filter((t) => t.user_id !== user.id) : []
  const showNodeTab = isOwnerSide

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-slate-900">
            技术支持 <span className="text-sm font-normal text-slate-400">Support</span>
          </h1>
          <p className="mt-0.5 text-sm text-slate-500">
            针对你的实例提交工单，宿主机技术支持将直接处理并回复
          </p>
        </div>
        <Button onClick={openCreate}>
          <SendIcon className="h-4 w-4" />
          提交工单
        </Button>
      </div>

      {actionError && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">{actionError}</div>
      )}

      {/* 我的工单 */}
      <Card>
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
          <span className="text-sm font-semibold text-slate-800">我的工单（{myTickets.length}）</span>
        </div>
        {myTickets.length === 0 ? (
          <Empty text="还没有工单，遇到问题点击右上角「提交工单」" icon={<HeadphonesIcon className="h-8 w-8" />} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                  <th className="px-5 py-3 font-medium">工单</th>
                  <th className="px-5 py-3 font-medium">实例</th>
                  <th className="px-5 py-3 font-medium">宿主机</th>
                  <th className="px-5 py-3 font-medium">状态</th>
                  <th className="px-5 py-3 font-medium">更新时间</th>
                  <th className="px-5 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {myTickets.map((t) => {
                  const unread = t.user_read === false && t.status === 'open'
                  return (
                    <tr key={t.id} className={cn('border-b border-slate-50 last:border-0 hover:bg-slate-50/60', unread && 'bg-lemon/30')}>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-2 font-medium text-slate-900">
                          {t.title}
                          {unread && <Badge color="red" dot>未读</Badge>}
                        </div>
                        <div className="mt-0.5 text-xs text-slate-400">#{t.id} · {t.content.slice(0, 60)}</div>
                      </td>
                      <td className="px-5 py-3 text-slate-500">{t.instance_name || '-'}</td>
                      <td className="px-5 py-3 text-slate-500">{t.node_name || '-'}</td>
                      <td className="px-5 py-3">{statusBadge(t)}</td>
                      <td className="px-5 py-3 font-mono text-xs text-slate-400">{fmtTime(t.updated_at)}</td>
                      <td className="px-5 py-3 text-right">
                        <Button size="sm" variant="sky" onClick={() => openDetail(t)}>查看/回复</Button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* 宿主机技术支持（处理侧）：路由到本节点/平台的工单 */}
      {showNodeTab && (
        <Card>
          <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
            <span className="text-sm font-semibold text-slate-800">
              宿主机技术支持工单
              <span className="ml-2 text-xs font-normal text-slate-400">用户针对你托管节点上的实例提交的工单</span>
            </span>
          </div>
          {nodeTickets.length === 0 ? (
            <Empty text="暂无待处理工单" />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-100 text-left text-xs text-slate-400">
                    <th className="px-5 py-3 font-medium">工单</th>
                    <th className="px-5 py-3 font-medium">用户</th>
                    <th className="px-5 py-3 font-medium">实例</th>
                    <th className="px-5 py-3 font-medium">状态</th>
                    <th className="px-5 py-3 font-medium">更新时间</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {nodeTickets.map((t) => {
                    const unread = t.node_read === false && t.status === 'open'
                    return (
                      <tr key={t.id} className={cn('border-b border-slate-50 last:border-0 hover:bg-slate-50/60', unread && 'bg-lemon/30')}>
                        <td className="px-5 py-3">
                          <div className="flex items-center gap-2 font-medium text-slate-900">
                            {t.title}
                            {unread && <Badge color="red" dot>未处理</Badge>}
                          </div>
                          <div className="mt-0.5 text-xs text-slate-400">#{t.id} · {t.content.slice(0, 60)}</div>
                        </td>
                        <td className="px-5 py-3 text-slate-500">{t.user_name || '-'}</td>
                        <td className="px-5 py-3 text-slate-500">{t.instance_name || '-'}</td>
                        <td className="px-5 py-3">{statusBadge(t)}</td>
                        <td className="px-5 py-3 font-mono text-xs text-slate-400">{fmtTime(t.updated_at)}</td>
                        <td className="px-5 py-3 text-right">
                          <Button size="sm" variant="sky" onClick={() => openDetail(t)}>处理/回复</Button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      {/* 新建工单弹窗 */}
      <Modal
        open={createOpen}
        title="提交工单"
        subtitle="选择你名下的实例，问题将送达该实例所在宿主机的技术支持"
        size="lg"
        onClose={() => { if (!creating) setCreateOpen(false) }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreateOpen(false)} disabled={creating}>取消</Button>
            <Button onClick={submitCreate} disabled={creating || !instanceId || !title.trim() || !content.trim()}>
              {creating ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '提交工单'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="关联实例" hint="只能为自己名下的实例提工单">
            <Select value={instanceId} onChange={(e) => setInstanceId(Number(e.target.value))}>
              <option value={0}>请选择实例…</option>
              {myInstances.map((inst) => (
                <option key={inst.id} value={inst.id}>
                  {inst.display_name || inst.name}（{inst.cpu_cores}C/{inst.memory_mb}M · {inst.node_name}）
                </option>
              ))}
            </Select>
            {myInstances.length === 0 && <p className="mt-1 text-xs text-slate-400">你还没有实例，无法提交工单</p>}
          </Field>
          <Field label="标题">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="例如：SSH 连接不上" maxLength={128} />
          </Field>
          <Field label="问题描述">
            <Textarea value={content} onChange={(e) => setContent(e.target.value)} placeholder="请描述遇到的问题、复现步骤等，宿主机技术支持将据此排查" rows={5} />
          </Field>
          {createError && <p className="text-sm text-red-600">{createError}</p>}
        </div>
      </Modal>

      {/* 工单详情弹窗 */}
      <Modal
        open={!!detail}
        size="lg"
        title={detail ? `工单 #${detail.id} · ${detail.title}` : ''}
        subtitle={detail ? `${detail.user_name || '用户'} 提交 · ${detail.instance_name || ''} · ${detail.node_name || ''}` : ''}
        onClose={() => { if (!replying) setDetail(null) }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDetail(null)} disabled={replying}>关闭</Button>
            {detail && detail.status === 'open' && isNodeSide(user, detail) && (
              <Button variant="sage" onClick={() => resolve(detail)}>
                <CheckIcon className="h-4 w-4" />
                标记已解决
              </Button>
            )}
            {detail && detail.status === 'resolved' && (
              <Button variant="outline" onClick={() => reopen(detail)}>重新打开</Button>
            )}
          </>
        }
      >
        {detailLoading ? (
          <div className="flex h-40 items-center justify-center">
            <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-blue-600" />
          </div>
        ) : detail ? (
          <div className="space-y-4">
            <div className="flex items-start gap-3">
              <Badge color={detail.status === 'resolved' ? 'green' : 'orange'} dot>
                {detail.status === 'resolved' ? '已解决' : '处理中'}
              </Badge>
              <p className="flex-1 rounded-xl border-2 border-black/10 bg-slate-50 px-4 py-3 text-sm leading-6 text-slate-700">
                {detail.content}
              </p>
            </div>

            <div className="space-y-3">
              {(detail.replies ?? []).map((r) => (
                <div key={r.id} className={cn('flex', r.user_id === user?.id ? 'justify-end' : 'justify-start')}>
                  <div className={cn(
                    'max-w-[85%] rounded-xl border-2 border-black/10 px-4 py-2.5',
                    r.user_id === user?.id ? 'bg-mint' : 'bg-white',
                  )}>
                    <div className="mb-1 flex items-center gap-2 text-xs text-slate-400">
                      <span className="font-medium text-slate-600">{r.user_name || '用户'}</span>
                      <span>{fmtDate(r.created_at)} {fmtTime(r.created_at)}</span>
                    </div>
                    <p className="text-sm leading-6 text-slate-700">{r.content}</p>
                  </div>
                </div>
              ))}
            </div>

            {detail.status === 'open' && (
              <div className="border-t border-slate-100 pt-4">
                <Field label={isNodeSide(user, detail) ? '回复用户' : '补充说明'}>
                  <Textarea value={replyText} onChange={(e) => setReplyText(e.target.value)} rows={3} placeholder="输入回复内容…" />
                </Field>
                <div className="mt-3 flex justify-end">
                  <Button onClick={submitReply} disabled={replying || !replyText.trim()}>
                    {replying ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : '发送回复'}
                  </Button>
                </div>
              </div>
            )}
            {detail.status === 'resolved' && (
              <p className="rounded-lg bg-green-50 px-3 py-2 text-xs text-green-700">
                该工单已解决{detail.resolved_at ? `（${fmtDate(detail.resolved_at)}）` : ''}，如需继续沟通可点击「重新打开」。
              </p>
            )}
          </div>
        ) : (
          <p className="py-8 text-center text-sm text-slate-400">
            <ArrowLeftIcon className="mx-auto h-6 w-6" />
            加载失败
          </p>
        )}
      </Modal>
    </div>
  )
}
