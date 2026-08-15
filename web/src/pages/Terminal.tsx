import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { terminalWsUrl } from '../lib/api'
import { Button, Card, useToast } from '../components/ui'
import { ArrowLeftIcon, CopyIcon, PasteIcon } from '../components/icons'

// 自动重连次数上限（避免宿主机长时间离线时无限重试）。
// 可用 URL 参数覆盖：/terminal/:id?max_retries=<n>（0 = 无限重试）。
const DEFAULT_MAX_RETRIES = 20

function parseMaxRetries(v: string | null): number {
  if (v === null) return DEFAULT_MAX_RETRIES
  const n = Number(v)
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : DEFAULT_MAX_RETRIES
}

function isMac() {
  return /Mac|iPhone|iPad/.test(navigator.platform)
}

export default function Terminal() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<XTerm | null>(null)
  const toast = useToast()
  // 有选中文字时「复制」按钮可用。
  const [hasSelection, setHasSelection] = useState(false)
  // 在组件挂载时解析一次，重连逻辑稳定使用同一上限。
  const maxRetries = parseMaxRetries(searchParams.get('max_retries'))

  // 返回上一页；直接访问（无历史记录）时回到该实例详情页
  const goBack = () => {
    if (location.key !== 'default') {
      navigate(-1)
    } else {
      navigate(`/instances/${id}`)
    }
  }

  // 复制：优先 Clipboard API，失败时回退到隐藏 textarea + execCommand。
  const copySelection = async () => {
    const term = termRef.current
    if (!term) return
    const sel = term.getSelection()
    if (!sel) {
      toast('终端中没有选中的文字')
      return
    }
    try {
      await navigator.clipboard.writeText(sel)
      toast('已复制选中内容')
    } catch {
      const ta = document.createElement('textarea')
      ta.value = sel
      ta.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0'
      document.body.appendChild(ta)
      ta.focus()
      ta.select()
      let ok = false
      try {
        ok = document.execCommand('copy')
      } catch {
        ok = false
      }
      ta.remove()
      toast(ok ? '已复制选中内容' : '复制失败，请使用 Ctrl+Shift+C（Mac 为 Cmd+C）')
    }
  }

  // 粘贴：读取剪贴板并写入终端（安全上下文 / localhost 下可用）。
  const pasteClipboard = async () => {
    const term = termRef.current
    if (!term) return
    try {
      const text = await navigator.clipboard.readText()
      if (text) term.paste(text)
      else toast('剪贴板为空')
    } catch {
      toast('无法读取剪贴板（需 HTTPS 或 localhost，并允许剪贴板权限），请用 Ctrl+Shift+V 粘贴')
    }
  }

  useEffect(() => {
    if (!containerRef.current) return
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
      theme: { background: '#0f172a', foreground: '#e2e8f0', cursor: '#2563eb', selectionBackground: '#334155' },
      // 右键单击 = 选中光标下的单词（选词后即可复制）
      rightClickSelectsWord: true,
    })
    term.open(containerRef.current)
    termRef.current = term

    // 键盘复制/粘贴：
    //   Windows/Linux：Ctrl+Shift+C 复制选中，Ctrl+Shift+V 粘贴
    //   macOS：Cmd+C 复制选中，Cmd+V 粘贴（不拦截 Ctrl+C，保留中断信号）
    term.attachCustomKeyEventHandler((e) => {
      const mac = isMac()
      const copyMod = mac ? e.metaKey : e.ctrlKey
      const pasteMod = mac ? e.metaKey : e.ctrlKey
      if (e.type !== 'keydown') return true
      if (copyMod && (mac ? e.key.toLowerCase() === 'c' : e.shiftKey && e.key.toLowerCase() === 'c')) {
        if (term.getSelection()) {
          void copySelection()
        }
        return false
      }
      if (pasteMod && (mac ? e.key.toLowerCase() === 'v' : e.shiftKey && e.key.toLowerCase() === 'v')) {
        void pasteClipboard()
        return false
      }
      return true
    })

    // 有选中 → 启用「复制」按钮。
    const selSub = term.onSelectionChange(() => setHasSelection(!!term.getSelection()))

    // 自动重连：宿主机整机重启后 Agent 会有一段启动窗口（重建 NAT/IPv6 规则
    // 后才监听），单次连接失败时按 1s/2s/4s/8s… 退避重试，直到恢复。
    let closed = false
    let retry = 0
    let timer: number | undefined

    const connect = () => {
      if (closed) return
      const ws = new WebSocket(terminalWsUrl(Number(id)))
      const dataSub = term.onData((d) => {
        if (ws.readyState === WebSocket.OPEN) ws.send(d)
      })
      const resizeSub = term.onResize(({ cols, rows }) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'resize', cols, rows }))
        }
      })
      ws.onopen = () => {
        retry = 0
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }
      ws.onmessage = (e) => {
        term.write(e.data as string)
      }
      ws.onclose = () => {
        dataSub.dispose()
        resizeSub.dispose()
        if (closed) return
        retry++
        if (maxRetries > 0 && retry > maxRetries) {
          term.writeln(
            `\r\n\x1b[31m已达自动重连上限（${maxRetries} 次），已停止重试。请刷新页面重试，或检查实例/节点状态；可通过 /terminal/${id}?max_retries=<n> 调整上限（0 为无限）\x1b[0m`,
          )
          return
        }
        const delay = Math.min(1000 * 2 ** Math.min(retry - 1, 4), 8000)
        term.writeln(`\r\n\x1b[33m连接已断开，${(delay / 1000).toFixed(0)} 秒后自动重连（第 ${retry} 次）...\x1b[0m`)
        timer = window.setTimeout(connect, delay)
      }
    }
    connect()

    return () => {
      closed = true
      window.clearTimeout(timer)
      selSub.dispose()
      term.dispose()
      termRef.current = null
    }
  }, [id, maxRetries])

  const mac = isMac()
  const copyHint = mac ? '⌘C' : 'Ctrl+Shift+C'
  const pasteHint = mac ? '⌘V' : 'Ctrl+Shift+V'

  return (
    <div className="space-y-4">
      <header className="flex items-center gap-3">
        <div className="flex items-center gap-3">
          <Button variant="outline" size="sm" onClick={goBack} title="返回上一页">
            <ArrowLeftIcon className="h-4 w-4" />
            返回
          </Button>
          <div>
            <h1 className="text-lg font-semibold text-slate-900">
              网页终端 <span className="text-sm font-normal text-slate-400">Terminal</span>
            </h1>
            <p className="mt-0.5 text-sm text-slate-500">实例 #{id}</p>
          </div>
        </div>
      </header>
      <Card className="overflow-hidden p-0">
        <div className="flex items-center justify-between gap-2 border-b-2 border-black/10 bg-slate-50 px-3 py-2">
          <p className="font-mono text-[11px] text-slate-400">
            鼠标拖选复制文字 · 右键选中单词 · {copyHint} 复制 · {pasteHint} 粘贴
          </p>
          <div className="flex items-center gap-1.5">
            <Button variant="outline" size="sm" onClick={copySelection} disabled={!hasSelection} title={`复制选中文字（${copyHint}）`}>
              <CopyIcon className="h-3.5 w-3.5" />
              复制
            </Button>
            <Button variant="outline" size="sm" onClick={pasteClipboard} title={`粘贴剪贴板内容（${pasteHint}）`}>
              <PasteIcon className="h-3.5 w-3.5" />
              粘贴
            </Button>
          </div>
        </div>
        <div ref={containerRef} className="h-[calc(100vh-17rem)] min-h-96 bg-[#0f172a] p-2" />
      </Card>
    </div>
  )
}
