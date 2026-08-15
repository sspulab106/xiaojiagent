import { Card, CardContent } from '../components/ui'
import { InfoIcon } from '../components/icons'

export default function Placeholder({ title, en, description }: { title: string; en?: string; description?: string }) {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-lg font-semibold text-slate-900">
          {title} {en && <span className="text-sm font-normal text-slate-400">{en}</span>}
        </h1>
        {description && <p className="mt-0.5 text-sm text-slate-500">{description}</p>}
      </header>
      <Card>
        <CardContent className="flex flex-col items-center gap-3 py-20 text-center">
          <div className="text-slate-300">
            <InfoIcon className="h-10 w-10" />
          </div>
          <div className="text-base font-medium text-slate-700">该功能开发中，敬请期待。</div>
          <p className="text-sm text-slate-400">我们正在积极筹备，很快与大家见面。</p>
        </CardContent>
      </Card>
    </div>
  )
}
