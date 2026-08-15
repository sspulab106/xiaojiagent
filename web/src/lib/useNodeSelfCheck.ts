import { useCallback, useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import { api, ApiError } from './api'
import type { Node, SelfCheckStatus } from './types'

// 解析节点记录的 last_selfcheck JSON 快照；空/损坏时返回 null。
export interface SelfCheckMeta {
  status?: string
  run_id?: string
  exit_code?: number
  started_at?: string
  finished_at?: string
  output?: string
}

export function parseSelfCheckMeta(node: Node): SelfCheckMeta | null {
  if (!node.last_selfcheck) return null
  try {
    return JSON.parse(node.last_selfcheck) as SelfCheckMeta
  } catch {
    return null
  }
}

export interface NodeSelfCheck {
  open: boolean
  node: Node | null
  run: { runId: string; status: string } | null
  output: string
  error: string
  starting: boolean
  bottomRef: RefObject<HTMLDivElement>
  start: (node: Node, onDone?: (st: SelfCheckStatus) => void) => Promise<void>
  close: () => void
}

/**
 * useNodeSelfCheck — 远程触发节点自检（verify-ndp.sh）并轮询回显输出。
 * 管理后台（admin=true）走 /admin/nodes/:id/selfcheck，托管中心（admin=false）
 * 走 /nodes/:id/selfcheck（机主自有节点）。
 */
export function useNodeSelfCheck(admin: boolean): NodeSelfCheck {
  const [open, setOpen] = useState(false)
  const [node, setNode] = useState<Node | null>(null)
  const [run, setRun] = useState<{ runId: string; status: string } | null>(null)
  const [output, setOutput] = useState('')
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)
  const timer = useRef<ReturnType<typeof setInterval> | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const stop = useCallback(() => {
    if (timer.current) {
      clearInterval(timer.current)
      timer.current = null
    }
  }, [])

  // 组件卸载时停止轮询
  useEffect(() => () => stop(), [stop])

  // 输出自动滚动到底部
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [output])

  const start = useCallback(
    async (n: Node, onDone?: (st: SelfCheckStatus) => void) => {
      if (starting) return // 防止连点触发多个自检
      setNode(n)
      setOpen(true)
      setRun(null)
      setOutput('')
      setError('')
      setStarting(true)
      stop()
      try {
        const res = admin ? await api.adminNodeSelfCheck(n.id) : await api.nodeSelfCheck(n.id)
        setRun({ runId: res.run_id, status: res.status })
        const poll = () => {
          const fetchSt = admin ? api.adminNodeSelfCheckStatus(n.id, res.run_id) : api.nodeSelfCheckStatus(n.id, res.run_id)
          fetchSt
            .then((st) => {
              setOutput(st.output)
              if (st.status === 'done' || st.status === 'failed') {
                setRun((prev) => (prev ? { ...prev, status: st.status } : prev))
                stop()
                onDone?.(st)
              }
            })
            .catch((err) => {
              setError(err instanceof ApiError ? err.message : '查询自检状态失败')
              stop()
            })
        }
        poll()
        timer.current = setInterval(poll, 2000)
      } catch (err) {
        setError(err instanceof ApiError ? err.message : '触发自检失败')
      } finally {
        setStarting(false)
      }
    },
    [admin, starting, stop],
  )

  const close = useCallback(() => {
    stop()
    setOpen(false)
    setNode(null)
    setRun(null)
    setOutput('')
    setError('')
    setStarting(false)
  }, [stop])

  return { open, node, run, output, error, starting, bottomRef, start, close }
}
