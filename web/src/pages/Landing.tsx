import { Link } from 'react-router-dom'
import { cn } from '../components/ui'

const FEATURES = [
  { no: '01', key: 'Speed', title: '秒级开通', desc: '容器引擎自动编排，从点击创建到 SSH 可用平均 0.8 秒，不等镜像构建。' },
  { no: '02', key: 'Price', title: '按内存计费', desc: '64MB 每月不到一美元，弹性扩缩实时生效，月底按用量结算。' },
  { no: '03', key: 'Global', title: '全球节点', desc: '香港、新加坡、日本、德国…… 12 个 BGP 优化节点，就近接入。' },
  { no: '04', key: 'NAT', title: '原生端口转发', desc: '每条规则独立 iptables DNAT，外部端口自由绑定，协议可选 TCP / UDP。' },
  { no: '05', key: 'Metering', title: '实时流量统计', desc: '出入站按月精确计量，控制台实时图表，超量自动限速不封机。' },
  { no: '06', key: 'Terminal', title: '浏览器内终端', desc: 'WebSocket 直连容器 PTY，无需安装任何 SSH 工具，弱网也能用。' },
]

const PLANS = [
  {
    name: 'Spark',
    price: '$0.99',
    spec: [
      ['内存', '64 MB'],
      ['磁盘', '1 GB'],
      ['端口', '1'],
      ['流量', '200 GB'],
    ],
    featured: false,
  },
  {
    name: 'Server',
    price: '$4.99',
    spec: [
      ['内存', '512 MB'],
      ['磁盘', '5 GB'],
      ['端口', '5'],
      ['流量', '1,000 GB'],
    ],
    featured: true,
  },
  {
    name: 'Pro',
    price: '$14.99',
    spec: [
      ['内存', '2 GB'],
      ['磁盘', '20 GB'],
      ['端口', '20'],
      ['流量', '无限'],
    ],
    featured: false,
  },
]

// 新粗野主义按钮交互
const cta = [
  'inline-flex items-center justify-center gap-1.5 border-2 border-black font-bold',
  'shadow-hard-sm transition-all duration-100',
  'hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard',
  'active:translate-x-[2px] active:translate-y-[2px] active:shadow-none',
].join(' ')

function NarwhalMark() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 2C7 2 3 5.5 3 10c0 3.2 1.9 5.9 4.7 7.2L9 20h6l1.3-2.8C19.1 15.9 21 13.2 21 10c0-4.5-4-8-9-8Z"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinejoin="round"
      />
      <circle cx="12" cy="10" r="3.2" stroke="#4338CA" strokeWidth="2.4" />
    </svg>
  )
}

export default function Landing() {
  return (
    <div className="min-h-screen bg-[#E0E7FF] text-ink antialiased">
      <header className="sticky top-0 z-40 border-b-[2.5px] border-black bg-white/95 backdrop-blur-sm">
        <nav className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4 sm:px-6" aria-label="主导航">
          <Link to="/" className="flex items-center gap-2">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl border-2 border-black bg-sage text-black shadow-hard-sm">
              <NarwhalMark />
            </span>
            <span className="text-sm font-extrabold tracking-tight">
              独角鲸云{' '}
              <span className="ml-1 font-mono text-[10px] font-bold uppercase tracking-widest text-muted">
                Narwhal Cloud
              </span>
            </span>
          </Link>
          <div className="hidden items-center gap-7 text-sm font-medium text-body md:flex">
            <a href="#features" className="transition-colors hover:text-ink">产品</a>
            <a href="#pricing" className="transition-colors hover:text-ink">价格</a>
            <a href="#docs" className="transition-colors hover:text-ink">文档</a>
          </div>
          <div className="flex items-center gap-3">
            <Link to="/login" className="hidden text-sm font-bold text-body transition-colors hover:text-ink sm:block">
              登录
            </Link>
            <Link
              to="/"
              className={cnCta('rounded-xl bg-mint px-3.5 py-1.5 text-sm text-black')}
            >
              进入控制台
            </Link>
          </div>
        </nav>
      </header>

      <main>
        {/* Hero */}
        <section className="mx-auto max-w-6xl px-4 pt-20 pb-16 sm:px-6 sm:pt-28" aria-labelledby="hero-title">
          <p className="mb-5 inline-block rounded-full border-2 border-black bg-lemon px-3 py-1 font-mono text-xs font-bold text-black shadow-hard-sm">
            ✨ NAT VPS · 容器级隔离 · 秒级开通
          </p>
          <h1 id="hero-title" className="max-w-2xl text-4xl font-extrabold leading-[1.1] tracking-tight sm:text-6xl">
            一台 VPS 的钱，
            <br />
            开一排<span className="text-brand">容器</span>。
          </h1>
          <p className="mt-5 max-w-xl text-base font-medium leading-relaxed text-body sm:text-lg">
            在宿主机上以容器隔离运行你的 Linux 环境。64MB 起售，按内存付费，端口转发、Web 终端、实时流量统计开箱即用。
          </p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link
              to="/instances?create=1"
              className={cnCta('rounded-xl bg-mint px-5 py-2.5 text-sm text-black')}
            >
              立即创建实例
            </Link>
            <a
              href="#pricing"
              className={cnCta('rounded-xl bg-white px-5 py-2.5 text-sm text-ink')}
            >
              查看价格
            </a>
          </div>
          <dl className="mt-12 flex flex-wrap gap-x-10 gap-y-3 border-t-2 border-black/15 pt-6">
            <div className="flex items-baseline gap-2"><dt className="text-xs font-bold text-muted">已开通实例</dt><dd className="font-mono text-sm font-extrabold">1,284</dd></div>
            <div className="flex items-baseline gap-2"><dt className="text-xs font-bold text-muted">全球节点</dt><dd className="font-mono text-sm font-extrabold">12</dd></div>
            <div className="flex items-baseline gap-2"><dt className="text-xs font-bold text-muted">平均开通耗时</dt><dd className="font-mono text-sm font-extrabold">0.8s</dd></div>
            <div className="flex items-baseline gap-2"><dt className="text-xs font-bold text-muted">可用性</dt><dd className="font-mono text-sm font-extrabold text-ok">99.98%</dd></div>
          </dl>
        </section>

        {/* 终端块 */}
        <section className="mx-auto max-w-6xl px-4 sm:px-6" aria-label="快速开始">
          <div className="overflow-hidden rounded-2xl border-[2.5px] border-black bg-[#0B0D0F] shadow-hard-lg">
            <div className="flex items-center gap-1.5 border-b-2 border-white/10 px-4 py-2.5">
              <span className="h-2.5 w-2.5 rounded-full border border-black bg-[#FF5F57]" />
              <span className="h-2.5 w-2.5 rounded-full border border-black bg-[#FEBC2E]" />
              <span className="h-2.5 w-2.5 rounded-full border border-black bg-[#28C840]" />
              <span className="ml-3 font-mono text-[11px] text-white/40">quickstart — zsh</span>
            </div>
            <div className="narwhal-scroll overflow-x-auto p-5 font-mono text-[13px] leading-7">
              <p className="text-white/30"># 安装 agent 到你的宿主机</p>
              <p><span className="text-white/40">$</span>{' '}<span className="text-white">curl -fsSL https://panel.narwhal.cloud/install.sh</span>{' '}<span className="text-white">| sudo bash</span></p>
              <p className="text-[#4ADE80]">✓ agent 已安装 · 3s 后节点上线</p>
              <p className="mt-2 text-white/30"># 一键开一台 debian，64MB 起</p>
              <p><span className="text-white/40">$</span>{' '}<span className="text-white">narwhal create debian --mem 512m --disk 5g</span></p>
              <p className="text-[#4ADE80]">✓ 实例已创建</p>
              <p className="text-white">ssh <span className="text-[#7DD3FC]">root@hk-01.narwhal.cloud</span> -p 20007</p>
            </div>
          </div>
        </section>

        {/* 功能网格 */}
        <section id="features" className="mx-auto max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="features-title">
          <p className="mb-3 inline-block rounded-full border-2 border-black bg-peach px-3 py-1 font-mono text-xs font-bold text-black shadow-hard-sm">
            // features
          </p>
          <h2 id="features-title" className="text-3xl font-extrabold tracking-tight">开箱即用，无需折腾</h2>
          <div className="mt-10 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {FEATURES.map((f) => (
              <article
                key={f.no}
                className="rounded-2xl border-2 border-black bg-white p-6 shadow-hard transition-all duration-100 hover:-translate-y-1 hover:shadow-hard-lg"
              >
                <p className="inline-block rounded-full border-2 border-black bg-sky-200 px-2.5 py-0.5 font-mono text-[11px] font-bold uppercase tracking-widest text-black">
                  {f.no} · {f.key}
                </p>
                <h3 className="mt-3 text-base font-extrabold">{f.title}</h3>
                <p className="mt-2 text-sm font-medium leading-relaxed text-body">{f.desc}</p>
              </article>
            ))}
          </div>
        </section>

        {/* 价格 */}
        <section id="pricing" className="border-y-2 border-black bg-[#D6DDFF]">
          <div className="mx-auto max-w-6xl px-4 py-20 sm:px-6" aria-labelledby="pricing-title">
            <p className="mb-3 inline-block rounded-full border-2 border-black bg-sage px-3 py-1 font-mono text-xs font-bold text-black shadow-hard-sm">
              // pricing
            </p>
            <h2 id="pricing-title" className="text-3xl font-extrabold tracking-tight">透明定价，按内存付费</h2>
            <div className="mt-10 grid grid-cols-1 gap-6 md:grid-cols-3">
              {PLANS.map((plan) => (
                <article
                  key={plan.name}
                  className={
                    plan.featured
                      ? 'relative rounded-2xl border-[3px] border-black bg-white p-6 shadow-hard-lg md:-translate-y-2'
                      : 'rounded-2xl border-2 border-black bg-white p-6 shadow-hard transition-all duration-100 hover:-translate-y-1 hover:shadow-hard-lg'
                  }
                >
                  <p className="flex items-center justify-between">
                    <span className="font-mono text-[11px] font-bold uppercase tracking-widest text-muted">
                      {plan.name}
                    </span>
                    {plan.featured && (
                      <span className="rounded-full border-2 border-black bg-lemon px-2 py-0.5 text-[11px] font-bold text-black">
                        最受欢迎
                      </span>
                    )}
                  </p>
                  <p className="mt-4 flex items-baseline gap-1">
                    <span className="font-mono text-3xl font-extrabold">{plan.price}</span>
                    <span className="text-sm font-medium text-muted">/月</span>
                  </p>
                  <ul className="mt-5 space-y-2.5 text-sm font-medium text-body">
                    {plan.spec.map(([k, v]) => (
                      <li key={k} className="flex justify-between border-b border-dashed border-black/15 pb-2">
                        <span>{k}</span>
                        <span className="font-mono font-bold">{v}</span>
                      </li>
                    ))}
                  </ul>
                  <Link
                    to="/instances?create=1"
                    className={
                      plan.featured
                        ? cnCta('mt-6 block rounded-xl bg-mint py-2 text-center text-sm text-black')
                        : cnCta('mt-6 block rounded-xl bg-white py-2 text-center text-sm text-ink')
                    }
                  >
                    选择 {plan.name}
                  </Link>
                </article>
              ))}
            </div>
          </div>
        </section>
      </main>

      <footer id="docs" className="mx-auto max-w-6xl px-4 py-12 sm:px-6">
        <div className="flex flex-col items-start justify-between gap-8 border-t-2 border-black/15 pt-10 md:flex-row">
          <div>
            <p className="text-sm font-extrabold">
              独角鲸云{' '}
              <span className="ml-1 font-mono text-[10px] font-bold uppercase tracking-widest text-muted">Narwhal Cloud</span>
            </p>
            <p className="mt-2 max-w-xs text-sm font-medium leading-relaxed text-body">
              多租户容器 / NAT VPS 平台。文档见 <span className="font-mono text-[13px] font-bold text-brand">docs/</span>
              ，SDK 支持 Go / Python / CLI。
            </p>
          </div>
          <div className="grid grid-cols-3 gap-12 text-sm">
            <div className="space-y-2.5">
              <p className="text-xs font-extrabold uppercase tracking-widest text-muted">产品</p>
              <a href="#features" className="block font-medium text-body hover:text-ink">实例</a>
              <a href="#pricing" className="block font-medium text-body hover:text-ink">端口转发</a>
              <a href="#pricing" className="block font-medium text-body hover:text-ink">Web 终端</a>
            </div>
            <div className="space-y-2.5">
              <p className="text-xs font-extrabold uppercase tracking-widest text-muted">资源</p>
              <a href="#" className="block font-medium text-body hover:text-ink">文档</a>
              <a href="#" className="block font-medium text-body hover:text-ink">API 参考</a>
              <a href="#" className="block font-medium text-body hover:text-ink">状态页</a>
            </div>
            <div className="space-y-2.5">
              <p className="text-xs font-extrabold uppercase tracking-widest text-muted">公司</p>
              <a href="#" className="block font-medium text-body hover:text-ink">关于</a>
              <a href="#" className="block font-medium text-body hover:text-ink">联系方式</a>
              <a href="#" className="block font-medium text-body hover:text-ink">服务条款</a>
            </div>
          </div>
        </div>
        <p className="mt-10 font-mono text-xs font-medium text-muted">© 2026 Narwhal Cloud · Proudly containerized</p>
      </footer>
    </div>
  )
}

// 复用 ui.tsx 的 cn 帮助函数
function cnCta(...parts: (string | false | undefined | null)[]) {
  return cn(cta, ...parts)
}
