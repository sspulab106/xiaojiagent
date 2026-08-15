import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, ApiError, getToken, setToken } from '../lib/api'
import type { User } from '../lib/types'
import { cn, Kbd } from './ui'
import {
  CreditCardIcon,
  DashboardIcon,
  FishIcon,
  GlobeIcon,
  HeadphonesIcon,
  LogOutIcon,
  PlusCircleIcon,
  ServerIcon,
  ShieldIcon,
  StoreIcon,
} from './icons'

const PAGE_TITLES: Array<{ re: RegExp; title: string; en: string }> = [
  { re: /^\/$/, title: '控制面板', en: 'Dashboard' },
  { re: /^\/instances\/\d+\/terminal/, title: '网页终端', en: 'Terminal' },
  { re: /^\/instances\/\d+/, title: '实例详情', en: 'Instance Detail' },
  { re: /^\/instances/, title: '实例管理', en: 'Instances' },
  { re: /^\/hosting/, title: '托管中心', en: 'Hosting Center' },
  { re: /^\/recharge/, title: '账单充值', en: 'Billing' },
  { re: /^\/ip/, title: '专属 IP', en: 'Dedicated IP' },
  { re: /^\/market/, title: '交易市场', en: 'Marketplace' },
  { re: /^\/support/, title: '技术支持', en: 'Support' },
  { re: /^\/profile/, title: '个人信息', en: 'Profile' },
  { re: /^\/admin/, title: '管理后台', en: 'Admin' },
]

function pageMeta(pathname: string): { title: string; en: string } {
  for (const p of PAGE_TITLES) {
    if (p.re.test(pathname)) return { title: p.title, en: p.en }
  }
  return { title: '独角鲸云', en: 'Narwhal Cloud' }
}

interface NavItem {
  to: string
  label: string
  kbd: string
  icon: React.ReactNode
}

const NAV_GROUPS: Array<{ label: string; items: NavItem[] }> = [
  {
    label: '概览',
    items: [{ to: '/', label: '控制面板', kbd: '/', icon: <DashboardIcon /> }],
  },
  {
    label: '资源',
    items: [
      { to: '/instances', label: '实例管理', kbd: 'n', icon: <ServerIcon /> },
      { to: '/hosting', label: '托管中心', kbd: 'h', icon: <PlusCircleIcon /> },
      { to: '/ip', label: '专属 IP', kbd: 'i', icon: <GlobeIcon /> },
      { to: '/market', label: '交易市场', kbd: 'm', icon: <StoreIcon /> },
    ],
  },
  {
    label: '账户',
    items: [
      { to: '/recharge', label: '账单充值', kbd: 'b', icon: <CreditCardIcon /> },
      { to: '/support', label: '技术支持', kbd: '?', icon: <HeadphonesIcon /> },
    ],
  },
]

export default function Layout({ children }: { children?: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [warnNoNode, setWarnNoNode] = useState(false)
  const [profileError, setProfileError] = useState('')
  const [ticketUnread, setTicketUnread] = useState(0)
  const location = useLocation()
  const navigate = useNavigate()
  const retryTimer = useRef<number | undefined>(undefined)
  const unreadTimer = useRef<number | undefined>(undefined)

  // 未读工单气泡：管理员/机主看 node_unread，普通用户看 user_unread。
  const loadTicketUnread = useCallback(() => {
    if (!getToken()) return
    api
      .ticketUnread()
      .then((u) => setTicketUnread(u.node_unread + u.user_unread))
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!getToken()) return
    loadTicketUnread()
    unreadTimer.current = window.setInterval(loadTicketUnread, 30000)
    return () => window.clearInterval(unreadTimer.current)
  }, [loadTicketUnread])

  // Loads the profile. A real 401 (token expired/revoked) drops the session;
  // transient failures (network, 5xx, proxy hiccup) keep the stored token and
  // auto-retry so a plain page refresh never logs the user out.
  const loadProfile = useCallback(() => {
    api
      .profile()
      .then((u) => {
        setProfileError('')
        setUser(u)
        if (u.role === 'admin') {
          api
            .adminStats()
            .then((s) => setWarnNoNode(s.nodes_online === 0))
            .catch(() => {})
        }
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          setToken(null)
          navigate('/login')
          return
        }
        setProfileError('连接服务器失败，正在重试…')
        retryTimer.current = window.setTimeout(loadProfile, 3000)
      })
  }, [navigate])

  useEffect(() => {
    loadProfile()
    return () => window.clearTimeout(retryTimer.current)
  }, [loadProfile])

  const isAdmin = user?.role === 'admin'
  const groups = isAdmin
    ? [
        ...NAV_GROUPS,
        { label: '管理', items: [{ to: '/admin', label: '管理后台', kbd: 'a', icon: <ShieldIcon /> }] },
      ]
    : NAV_GROUPS

  const logout = () => {
    setToken(null)
    navigate('/login')
  }

  const avatarText = (user?.username ?? 'SSPU').slice(0, 4).toUpperCase()
  const { title, en } = pageMeta(location.pathname)

  return (
    <div className="flex h-full min-h-screen">
      {warnNoNode && (
        <div className="fixed top-0 inset-x-0 z-50 border-b-2 border-black bg-rose-300 px-4 py-1.5 text-center text-xs font-bold text-black">
          暂无在线节点，请及时在管理后台检查节点状态
        </div>
      )}
      {profileError && (
        <div className="fixed top-0 inset-x-0 z-50 border-b-2 border-black bg-lemon px-4 py-1.5 text-center text-xs font-bold text-black">
          {profileError}
        </div>
      )}

      {/* 侧边栏 */}
      <aside className="fixed inset-y-0 left-0 z-40 flex w-16 flex-col border-r-[2.5px] border-black bg-white md:w-56">
        <Link to="/" className="flex h-14 shrink-0 items-center gap-2.5 border-b-2 border-black px-3 md:px-4">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border-2 border-black bg-lavender text-black shadow-hard-sm">
            <FishIcon className="h-5 w-5" />
          </div>
          <div className="hidden min-w-0 md:block">
            <div className="text-sm font-extrabold leading-tight text-ink">独角鲸云</div>
            <div className="font-mono text-[10px] font-bold uppercase tracking-[0.18em] text-muted">
              Narwhal Cloud
            </div>
          </div>
        </Link>

        <nav className="narwhal-scroll flex-1 overflow-y-auto px-2 py-3 md:px-3">
          {groups.map((group) => (
            <div key={group.label} className="mb-1">
              <p className="hidden px-2 pb-1.5 pt-2 font-mono text-[10px] font-bold uppercase tracking-widest text-muted md:block">
                {group.label}
              </p>
              <div className="space-y-1">
                {group.items.map((item) => (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    className={({ isActive }) =>
                      cn(
                        'flex w-full items-center justify-center gap-2.5 rounded-xl border-2 px-2 py-2 text-sm font-bold transition-all duration-100 md:justify-between',
                        isActive
                          ? 'border-black bg-mint text-black shadow-hard-sm'
                          : 'border-transparent text-body hover:bg-white hover:text-ink hover:shadow-hard-sm',
                      )
                    }
                  >
                    <span className="flex items-center gap-2.5">
                      <span className="shrink-0">{item.icon}</span>
                      <span className="hidden md:inline">{item.label}</span>
                      {item.to === '/support' && ticketUnread > 0 && (
                        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full border-2 border-black bg-rose-300 px-1 font-mono text-[10px] font-bold text-black">
                          {ticketUnread > 99 ? '99+' : ticketUnread}
                        </span>
                      )}
                    </span>
                    <Kbd className="hidden md:inline-flex">{item.kbd}</Kbd>
                  </NavLink>
                ))}
              </div>
            </div>
          ))}
        </nav>

        {/* 用户区 */}
        <div className="shrink-0 border-t-2 border-black p-3">
          <Link
            to="/profile"
            className="flex items-center gap-2.5 rounded-xl px-2 py-2 transition-all duration-100 hover:bg-white hover:shadow-hard-sm"
            title="个人信息管理"
          >
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border-2 border-black bg-lavender font-mono text-xs font-bold text-black">
              {avatarText}
            </div>
            <div className="hidden min-w-0 md:block">
              <div className="truncate text-sm font-bold text-ink">{user?.username ?? '...'}</div>
              <div className="font-mono text-[11px] font-medium text-muted">
                {user?.role === 'admin' ? 'admin' : 'user'} · 个人信息
              </div>
            </div>
          </Link>
          <button
            onClick={logout}
            className="mt-1 flex w-full items-center justify-center gap-2 rounded-xl border-2 border-transparent px-2 py-2 text-sm font-bold text-danger transition-all duration-100 hover:border-black hover:bg-rose-300 hover:text-black hover:shadow-hard-sm md:justify-start md:px-3"
          >
            <LogOutIcon className="h-4 w-4 shrink-0" />
            <span className="hidden md:inline">退出登录</span>
          </button>
        </div>
      </aside>

      {/* 主区域 */}
      <div className="flex w-full flex-col pl-16 md:pl-56">
        <header className="flex h-14 shrink-0 items-center justify-between border-b-[2.5px] border-black bg-white px-4 md:px-6">
          <h1 className="text-base font-extrabold text-ink">
            {title}
            <span className="ml-2 hidden font-mono text-xs font-normal text-muted sm:inline">{en}</span>
          </h1>
          <div className="flex items-center gap-2.5">
            <Link
              to="/recharge"
              className="hidden rounded-xl border-2 border-black bg-white px-3 py-1.5 text-xs font-bold text-ink shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none sm:block"
            >
              充值
            </Link>
            <Link
              to="/instances?create=1"
              className="rounded-xl border-2 border-black bg-mint px-3 py-1.5 text-xs font-bold text-black shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none"
            >
              新建实例
            </Link>
          </div>
        </header>
        <main className="flex-1 overflow-y-auto p-4 md:p-6">
          <div className="mx-auto max-w-7xl">
            {children ?? <Outlet />}
          </div>
        </main>
      </div>
    </div>
  )
}
