import { useId } from 'react'
import { cn } from './ui'

// 零依赖 SVG 图表：实时资源监控用（尊重 prefers-reduced-motion，CSS 过渡由
// index.css 的全局 reduced-motion 规则兜底）。

// 将 y 轴上限取整到 1/2/5×10^k 的“nice”刻度：采样值小幅波动时图表不再
// 每 30s 重新缩放一次，消除曲线上下抽动/页面抖动。
function niceCeil(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 1
  const exp = Math.floor(Math.log10(v))
  const base = Math.pow(10, exp)
  const frac = v / base
  const nice = frac <= 1 ? 1 : frac <= 2 ? 2 : frac <= 5 ? 5 : 10
  return nice * base
}

// HistorySparkline renders a rolling sample buffer as an SVG area line chart.
export function HistorySparkline({
  data,
  color = '#0066FF',
  height = 44,
  max,
  label,
  value,
}: {
  data: number[]
  color?: string
  height?: number
  max?: number
  label?: string
  value?: string
}) {
  const uid = useId()
  const w = 160
  const pts = data.length
  const hi = max && max > 0 ? max : niceCeil(Math.max(...data, 1))
  const step = pts > 1 ? w / (pts - 1) : w
  const y = (v: number) => height - 2 - Math.max(0, Math.min(1, v / hi)) * (height - 6)
  const line = data.map((v, i) => `${(i * step).toFixed(1)},${y(v).toFixed(1)}`).join(' ')
  const area = `0,${height} ${line} ${w},${height}`
  return (
    <div>
      {(label || value) && (
        <div className="mb-1 flex items-baseline justify-between">
          <span className="text-xs text-muted">{label}</span>
          {value && <span className="font-mono text-xs font-medium text-ink">{value}</span>}
        </div>
      )}
      <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" className="w-full" style={{ height }} role="img" aria-label={label}>
        <defs>
          <linearGradient id={uid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.28" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        <polygon points={area} fill={`url(#${uid})`} />
        <polyline
          points={line}
          fill="none"
          stroke={color}
          strokeWidth="1.6"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      </svg>
    </div>
  )
}

// LiveBar is an animated horizontal usage bar (transition-all driven).
export function LiveBar({
  value,
  max,
  color = 'bg-brand',
  className,
}: {
  value: number
  max: number
  color?: string
  className?: string
}) {
  const pct = Math.max(0, Math.min(100, max > 0 ? (value / max) * 100 : 0))
  return (
    <div className={cn('h-1.5 w-full overflow-hidden rounded-full bg-well', className)}>
      <div
        className={cn('h-full rounded-full transition-all duration-700 ease-out', color)}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

// useHistory is a tiny rolling-buffer hook for real-time chart samples.
export function useHistory(maxLen = 20) {
  const push = (buf: number[], v: number): number[] => {
    const next = [...buf, v]
    return next.length > maxLen ? next.slice(next.length - maxLen) : next
  }
  return { push }
}
