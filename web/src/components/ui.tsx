import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react'
import { AlertIcon, CheckIcon, ChevronDownIcon, CopyIcon, XIcon } from './icons'

export function cn(...parts: (string | false | undefined | null)[]) {
  return parts.filter(Boolean).join(' ')
}

// 新粗野主义通用交互：悬停抬起、按下压入（tactile）
const press = [
  'transition-all duration-100 ease-out',
  'hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard',
  'active:translate-x-[2px] active:translate-y-[2px] active:shadow-none',
].join(' ')

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger' | 'link' | 'lemon' | 'peach' | 'sage' | 'sky' | 'rose'
type ButtonSize = 'sm' | 'md' | 'lg' | 'icon'

const buttonVariants: Record<ButtonVariant, string> = {
  // 马卡龙薄荷绿 = 主操作
  primary: 'bg-mint text-black hover:bg-[#B4E84B]',
  // 薰衣草紫 = 次操作
  secondary: 'bg-lavender text-black hover:bg-[#CE9CFE]',
  // 柠檬黄：强调/高亮操作
  lemon: 'bg-lemon text-black hover:bg-[#FDE68A]',
  // 蜜桃橙：警告/温暖操作
  peach: 'bg-peach text-black hover:bg-[#FDCE9E]',
  // 鼠尾草绿：成功/确认类操作
  sage: 'bg-sage text-black hover:bg-[#A7F3D0]',
  // 天空蓝：信息类操作
  sky: 'bg-sky-200 text-black hover:bg-sky-300',
  // 玫瑰红：强调的危险确认
  rose: 'bg-rose-300 text-black hover:bg-rose-400',
  outline: 'bg-white text-ink hover:bg-slate-50',
  // 幽灵按钮：无边框无阴影，悬浮时才浮现
  ghost: 'border-transparent bg-transparent text-ink shadow-none hover:border-black hover:bg-white hover:shadow-hard-sm active:shadow-none',
  danger: 'bg-[#F87171] text-black hover:bg-[#FCA5A5]',
  link: 'border-0 bg-transparent text-brand shadow-none hover:translate-x-0 hover:translate-y-0 hover:shadow-none hover:underline active:shadow-none',
}

const buttonSizes: Record<ButtonSize, string> = {
  sm: 'h-8 rounded-lg px-3 text-xs',
  md: 'h-10 rounded-xl px-4 text-sm',
  lg: 'h-11 rounded-xl px-6 text-base',
  icon: 'h-10 w-10 rounded-xl',
}

export function Button({
  children,
  variant = 'primary',
  size = 'md',
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
}) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-1.5 whitespace-nowrap font-bold select-none',
        'border-[2.5px] border-black shadow-hard-sm',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-black focus-visible:ring-offset-2',
        'disabled:pointer-events-none disabled:opacity-50 disabled:shadow-none [&_svg]:shrink-0',
        press,
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Card
// ---------------------------------------------------------------------------

export function Card({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('rounded-2xl border-[2.5px] border-black bg-white shadow-hard', className)}>
      {children}
    </div>
  )
}

export function CardHeader({
  title,
  subtitle,
  right,
  className,
}: {
  title: ReactNode
  subtitle?: ReactNode
  right?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-3 border-b-2 border-black/10 px-5 py-4', className)}>
      <div>
        <h3 className="text-base font-extrabold text-slate-900">{title}</h3>
        {subtitle && <p className="mt-0.5 text-xs font-medium text-slate-400">{subtitle}</p>}
      </div>
      {right}
    </div>
  )
}

export function CardContent({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('px-5 pb-5', className)}>{children}</div>
}

// ---------------------------------------------------------------------------
// Badge
// ---------------------------------------------------------------------------

type BadgeColor = 'green' | 'red' | 'slate' | 'indigo' | 'blue' | 'orange' | 'purple'

const badgeMap: Record<BadgeColor, string> = {
  green: 'bg-sage text-black',
  red: 'bg-rose-300 text-black',
  slate: 'bg-[#E4E4E7] text-black',
  indigo: 'bg-indigo-300 text-black',
  blue: 'bg-sky-200 text-black',
  orange: 'bg-peach text-black',
  purple: 'bg-lavender text-black',
}

export function Badge({
  children,
  color = 'slate',
  dot,
  className,
}: {
  children: ReactNode
  color?: BadgeColor
  dot?: boolean
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border-2 border-black px-2.5 py-0.5 text-xs font-bold whitespace-nowrap',
        badgeMap[color],
        className,
      )}
    >
      {dot && <span className={cn('h-1.5 w-1.5 rounded-full', dotColor[color], color === 'green' && 'dot-pulse')} />}
      {children}
    </span>
  )
}

const dotColor: Record<BadgeColor, string> = {
  green: 'bg-green-500',
  red: 'bg-red-500',
  slate: 'bg-slate-400',
  indigo: 'bg-indigo-500',
  blue: 'bg-sky-500',
  orange: 'bg-amber-500',
  purple: 'bg-purple-500',
}

// ---------------------------------------------------------------------------
// Status pill / progress
// ---------------------------------------------------------------------------

export function StatusPill({ label, tone = 'slate', dot = true }: { label: string; tone?: BadgeColor; dot?: boolean }) {
  return (
    <Badge color={tone} dot={dot}>
      {label}
    </Badge>
  )
}

export function Progress({
  value,
  max,
  label,
  color,
}: {
  value: number
  max: number
  label?: string
  color?: 'blue' | 'red' | 'amber'
}) {
  const pct = Math.max(0, Math.min(100, max > 0 ? (value / max) * 100 : 0))
  const fill = color === 'red' ? 'bg-rose-300' : color === 'amber' ? 'bg-lemon' : 'bg-sky-300'
  return (
    <div className="w-full">
      {label && <div className="mb-1 flex items-center justify-between text-xs font-semibold text-slate-500">{label}</div>}
      <div className="h-3.5 w-full overflow-hidden rounded-full border-2 border-black bg-white shadow-[inset_1.5px_1.5px_0_0_rgba(0,0,0,0.12)]">
        <div
          className={cn('h-full rounded-full border-r-2 border-black/30 transition-all duration-300', fill)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Inputs
// ---------------------------------------------------------------------------

const inputCls =
  'h-10 w-full rounded-xl border-2 border-black bg-white px-3 text-sm font-medium text-ink shadow-hard-sm ' +
  'placeholder:text-slate-400 focus:border-black focus:outline-none focus:ring-2 focus:ring-black/60 focus:ring-offset-1'

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cn(inputCls, className)} />
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={cn('min-h-[84px] py-2 leading-6', inputCls, className)}
    />
  )
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={cn(inputCls, 'cursor-pointer', className)} />
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-bold text-slate-600">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs font-medium text-slate-400">{hint}</span>}
    </label>
  )
}

// ---------------------------------------------------------------------------
// Switch
// ---------------------------------------------------------------------------

export function Switch({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  label?: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative h-6 w-11 shrink-0 rounded-full border-2 border-black transition-colors disabled:opacity-50',
        checked ? 'bg-mint' : 'bg-white shadow-[inset_1.5px_1.5px_0_0_rgba(0,0,0,0.12)]',
      )}
    >
      <span
        className={cn(
          'absolute top-0.5 h-4 w-4 rounded-full border-2 border-black bg-white shadow-hard-sm transition-all',
          checked ? 'left-[22px]' : 'left-0.5',
        )}
      />
    </button>
  )
}

// ---------------------------------------------------------------------------
// Modal
// ---------------------------------------------------------------------------

export function Modal({
  open,
  title,
  subtitle,
  onClose,
  children,
  footer,
  size = 'md',
}: {
  open: boolean
  title: ReactNode
  subtitle?: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg'
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/40 backdrop-blur-[2px]" onClick={onClose} />
      <div
        className={cn(
          'relative w-full animate-pop-in rounded-2xl border-[2.5px] border-black bg-white shadow-hard-xl',
          size === 'sm' ? 'max-w-sm' : size === 'lg' ? 'max-w-2xl' : 'max-w-md',
        )}
      >
        <div className="flex items-start justify-between gap-4 border-b-2 border-black/10 px-5 py-4">
          <div>
            <h3 className="text-base font-extrabold text-slate-900">{title}</h3>
            {subtitle && <p className="mt-0.5 text-xs font-medium text-slate-400">{subtitle}</p>}
          </div>
          <button
            onClick={onClose}
            className="rounded-lg border-2 border-transparent p-1 text-slate-400 transition-all hover:border-black hover:bg-slate-50 hover:text-black"
          >
            <XIcon className="h-4 w-4" />
          </button>
        </div>
        <div className="narwhal-scroll max-h-[70vh] overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <div className="flex items-center justify-end gap-2 border-t-2 border-black/10 px-5 py-3">{footer}</div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// ConfirmDialog
// ---------------------------------------------------------------------------

export function ConfirmDialog({
  open,
  title,
  message,
  confirmText = '确认删除',
  cancelText = '取消',
  onConfirm,
  onClose,
}: {
  open: boolean
  title: ReactNode
  message?: ReactNode
  confirmText?: string
  cancelText?: string
  onConfirm: () => Promise<boolean> | boolean
  onClose: () => void
}) {
  const [pending, setPending] = useState(false)

  const handleConfirm = async () => {
    if (pending) return
    setPending(true)
    try {
      const ok = await onConfirm()
      if (ok) onClose()
    } finally {
      setPending(false)
    }
  }

  // 请求进行中禁止通过遮罩 / Esc / X 关闭
  const close = pending ? () => {} : onClose

  return (
    <Modal open={open} size="sm" title={title} onClose={close}>
      <div className="flex items-start gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border-2 border-black bg-rose-300 text-black">
          <AlertIcon className="h-5 w-5" />
        </div>
        <div className="flex-1 pt-0.5 text-sm font-medium leading-6 text-slate-600">{message}</div>
      </div>
      <div className="mt-5 -mx-5 -mb-4 flex items-center justify-end gap-2 border-t-2 border-black/10 px-5 py-3">
        <Button variant="ghost" onClick={close} disabled={pending}>
          {cancelText}
        </Button>
        <Button variant="danger" onClick={handleConfirm} disabled={pending}>
          {pending ? <div className="h-4 w-4 animate-spin rounded-full border-2 border-black/30 border-t-black" /> : confirmText}
        </Button>
      </div>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

export function Tabs({
  value,
  onChange,
  options,
  className,
}: {
  value: string
  onChange: (v: string) => void
  options: Array<{ key: string; label: ReactNode }>
  className?: string
}) {
  return (
    <div className={cn('inline-flex items-center gap-1 rounded-xl border-2 border-black bg-white p-1', className)}>
      {options.map((o) => (
        <button
          key={o.key}
          onClick={() => onChange(o.key)}
          className={cn(
            'rounded-lg px-3 py-1.5 text-xs font-bold transition-all duration-100',
            value === o.key
              ? 'bg-mint text-black shadow-hard-sm'
              : 'text-slate-500 hover:bg-slate-100 hover:text-black',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Accordion
// ---------------------------------------------------------------------------

export function Accordion({
  items,
  defaultOpen,
}: {
  items: Array<{ title: ReactNode; content: ReactNode }>
  defaultOpen?: number
}) {
  const [open, setOpen] = useState<number | null>(defaultOpen ?? 0)
  return (
    <div className="divide-y-2 divide-black/10 rounded-2xl border-2 border-black bg-white shadow-hard">
      {items.map((item, i) => {
        const isOpen = open === i
        return (
          <div key={i}>
            <button
              onClick={() => setOpen(isOpen ? null : i)}
              className="flex w-full items-center justify-between gap-3 px-5 py-3.5 text-left text-sm font-bold text-slate-900 transition-colors hover:bg-slate-50"
            >
              {item.title}
              <span className={cn('text-slate-400 transition-transform', isOpen && 'rotate-180')}>
                <ChevronDownIcon className="h-4 w-4" />
              </span>
            </button>
            {isOpen && <div className="px-5 pb-4">{item.content}</div>}
          </div>
        )
      })}
    </div>
  )
}

// ---------------------------------------------------------------------------
// CopyButton
// ---------------------------------------------------------------------------

export function CopyButton({
  value,
  label = '复制',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>()

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      const ta = document.createElement('textarea')
      ta.value = value
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setCopied(true)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => setCopied(false), 1600)
  }

  return (
    <button
      type="button"
      onClick={copy}
      className={cn(
        'inline-flex items-center gap-1 rounded-lg border-2 border-black bg-white px-2.5 py-1.5 text-xs font-bold text-ink shadow-hard-sm transition-all duration-100',
        'hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none',
        className,
      )}
    >
      {copied ? <CheckIcon className="h-3.5 w-3.5 text-green-600" /> : <CopyIcon className="h-3.5 w-3.5" />}
      {copied ? '已复制' : label}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Kbd
// ---------------------------------------------------------------------------

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return <kbd className={cn('kbd', className)}>{children}</kbd>
}

// ---------------------------------------------------------------------------
// Toast
// ---------------------------------------------------------------------------

type ToastFn = (message: string) => void

const ToastContext = createContext<ToastFn>(() => {})

export function useToast(): ToastFn {
  return useContext(ToastContext)
}

interface ToastItem {
  id: number
  message: string
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const idRef = useRef(0)

  const push = useCallback((message: string) => {
    const id = ++idRef.current
    setToasts((prev) => [...prev.slice(-3), { id, message }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 2400)
  }, [])

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div className="pointer-events-none fixed bottom-6 right-6 z-[100] flex flex-col items-end gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className="pointer-events-auto flex animate-toast-in items-center gap-2 rounded-xl border-2 border-black bg-white px-4 py-2.5 text-sm font-bold text-ink shadow-hard-lg"
          >
            <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-2 border-black bg-mint">
              <CheckIcon className="h-3 w-3" />
            </span>
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

export function Separator({ className }: { className?: string }) {
  return <div className={cn('h-0.5 bg-black/10', className)} />
}

export function Spinner({ className }: { className?: string }) {
  return (
    <div
      className={cn('h-5 w-5 animate-spin rounded-full border-2 border-black/20 border-t-black', className)}
    />
  )
}

export function Empty({ text, icon }: { text: string; icon?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 py-16 text-center">
      {icon && <div className="text-slate-300">{icon}</div>}
      <p className="text-sm font-medium text-slate-400">{text}</p>
    </div>
  )
}

// 马卡龙图标底色循环，让统计卡/装饰元素呈现更多色彩搭配
const pastelChips = ['bg-mint/50', 'bg-lavender/45', 'bg-lemon/50', 'bg-peach/50', 'bg-sage/50', 'bg-sky-200/70']

export function pastelTone(i: number): string {
  return pastelChips[((i % pastelChips.length) + pastelChips.length) % pastelChips.length]
}

export function StatCard({
  label,
  en,
  value,
  children,
  icon,
  tone,
}: {
  label: string
  en?: string
  value: ReactNode
  children?: ReactNode
  icon?: ReactNode
  tone?: string // pastel 底色类，如 'bg-mint/50'，默认薰衣草
}) {
  return (
    <Card className="p-5 transition-transform duration-100 hover:-translate-y-0.5">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-sm font-semibold text-slate-500">
          {icon && <span className={cn('flex h-6 w-6 items-center justify-center rounded-lg border-2 border-black text-brand', tone ?? 'bg-lavender/45')}>{icon}</span>}
          {label}
        </span>
        {en && <span className="text-[11px] font-bold uppercase tracking-wide text-slate-400">{en}</span>}
      </div>
      <div className="mt-2 font-mono text-2xl font-extrabold tabular-nums text-ink">{value}</div>
      {children && <div className="mt-2">{children}</div>}
    </Card>
  )
}

export function fmtBytes(n: number): string {
  if (!n || n < 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  if (n < 1024 ** 4) return `${(n / 1024 ** 3).toFixed(2)} GB`
  return `${(n / 1024 ** 4).toFixed(2)} TB`
}

export function fmtDate(s: string | null | undefined): string {
  if (!s) return '未设置'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function fmtTime(s: string | null | undefined): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
