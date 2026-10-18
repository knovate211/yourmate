import { Suspense, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Bell, ChevronDown, FileText, LayoutDashboard, LogOut, Menu, MessageCircle, Settings, Shield, UserRound, X } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useQuery } from '../lib/hooks'
import { useMessageEvents } from '../lib/realtime'
import { timeAgo } from '../lib/format'
import { gql } from '../lib/graphql'
import type { Notification } from '../lib/types'
import { Avatar, ButtonLink, PageLoader, cx } from './ui'

export function Logo({ className }: { className?: string }) {
  return (
    <Link to="/" className={cx('flex items-center gap-2', className)} aria-label="Your Mate home">
      <img src="/logo-mark.png" alt="" className="h-8 w-8" />
      <span className="text-lg font-bold tracking-tight text-ink-900">
        Your<span className="text-brand-500">Mate</span>
      </span>
    </Link>
  )
}

const navLinks = [
  { to: '/tutors', label: 'Find a tutor' },
  { to: '/guides', label: 'Campus guides' },
  { to: '/prep', label: 'Placement prep' },
  { to: '/communities', label: 'Communities' },
  { to: '/feed', label: 'Campus feed' },
]

function MessagesLink() {
  const { data, refetch } = useQuery<{ inboxSummary: { unread: number; requests: number } }>(`{ inboxSummary { unread requests } }`, undefined, { pollMs: 60_000 })
  useMessageEvents((e) => {
    if (!e.message.mine) refetch()
  })
  const count = (data?.inboxSummary.unread ?? 0) + (data?.inboxSummary.requests ?? 0)
  return (
    <Link to="/messages" className="relative rounded-lg p-2 text-ink-600 hover:bg-ink-100" aria-label={`Messages${count ? `, ${count} unread` : ''}`}>
      <MessageCircle className="h-5 w-5" />
      {count > 0 && (
        <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-brand-500 px-1 text-[10px] font-bold text-white">{count > 9 ? '9+' : count}</span>
      )}
    </Link>
  )
}

function NotificationBell() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  const { data, setData } = useQuery<{ notifications: { unreadCount: number; items: Notification[] } }>(
    `{ notifications(limit: 8) { unreadCount items { id title body link read createdAt } } }`,
    undefined,
    { pollMs: 30_000 },
  )
  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])
  const unread = data?.notifications.unreadCount ?? 0

  const openItem = async (n: Notification) => {
    setOpen(false)
    if (!n.read) {
      setData((d) => d && { notifications: { unreadCount: Math.max(0, d.notifications.unreadCount - 1), items: d.notifications.items.map((x) => (x.id === n.id ? { ...x, read: true } : x)) } })
      gql(`mutation($ids: [ID!]) { markNotificationsRead(ids: $ids) }`, { ids: [n.id] }).catch(() => {})
    }
    if (n.link) navigate(n.link)
  }

  return (
    <div className="relative" ref={ref}>
      <button onClick={() => setOpen((o) => !o)} className="relative rounded-lg p-2 text-ink-600 hover:bg-ink-100" aria-label={`Notifications${unread ? `, ${unread} unread` : ''}`}>
        <Bell className="h-5 w-5" />
        {unread > 0 && (
          <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-brand-500 px-1 text-[10px] font-bold text-white">{unread > 9 ? '9+' : unread}</span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-80 max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl border border-ink-200 bg-white shadow-lg">
          <div className="flex items-center justify-between border-b border-ink-100 px-4 py-2.5">
            <span className="text-sm font-semibold">Notifications</span>
            <Link to="/notifications" onClick={() => setOpen(false)} className="text-xs link">
              See all
            </Link>
          </div>
          {data?.notifications.items.length ? (
            <ul className="max-h-96 divide-y divide-ink-100 overflow-y-auto">
              {data.notifications.items.map((n) => (
                <li key={n.id}>
                  <button onClick={() => openItem(n)} className={cx('block w-full px-4 py-3 text-left hover:bg-ink-50', !n.read && 'bg-brand-50/60')}>
                    <p className="text-sm font-medium text-ink-900">{n.title}</p>
                    <p className="mt-0.5 line-clamp-2 text-xs text-ink-600">{n.body}</p>
                    <p className="mt-1 text-[11px] text-ink-400">{timeAgo(n.createdAt)}</p>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="px-4 py-6 text-center text-sm text-ink-500">You're all caught up.</p>
          )}
        </div>
      )}
    </div>
  )
}

function UserMenu() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])
  if (!user) return null
  const item = 'flex w-full items-center gap-2 px-4 py-2 text-sm text-ink-700 hover:bg-ink-50'
  return (
    <div className="relative" ref={ref}>
      <button onClick={() => setOpen((o) => !o)} className="flex items-center gap-1.5 rounded-full p-0.5 pr-2 hover:bg-ink-100" aria-label="Account menu">
        <Avatar name={user.displayName} url={user.avatarUrl} size={32} />
        <ChevronDown className="h-4 w-4 text-ink-500" />
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-56 overflow-hidden rounded-xl border border-ink-200 bg-white py-1 shadow-lg" onClick={() => setOpen(false)}>
          <div className="border-b border-ink-100 px-4 py-2.5">
            <p className="truncate text-sm font-semibold">{user.firstName} {user.lastName}</p>
            <p className="truncate text-xs text-ink-500">{user.campus?.name}</p>
          </div>
          <Link to="/profile" className={item}>
            <UserRound className="h-4 w-4" /> My profile
          </Link>
          <Link to="/dashboard" className={item}>
            <LayoutDashboard className="h-4 w-4" /> Dashboard
          </Link>
          <Link to="/resume-reviews" className={item}>
            <FileText className="h-4 w-4" /> Resume reviews
          </Link>
          <Link to="/settings" className={item}>
            <Settings className="h-4 w-4" /> Settings
          </Link>
          {user.isAdmin && (
            <Link to="/admin" className={item}>
              <Shield className="h-4 w-4" /> Admin
            </Link>
          )}
          <button
            className={cx(item, 'border-t border-ink-100')}
            onClick={async () => {
              await logout()
              navigate('/')
            }}
          >
            <LogOut className="h-4 w-4" /> Log out
          </button>
        </div>
      )}
    </div>
  )
}

function Nav() {
  const { user } = useAuth()
  const [mobile, setMobile] = useState(false)
  const location = useLocation()
  useEffect(() => {
    setMobile(false)
  }, [location.pathname])
  const linkCls = ({ isActive }: { isActive: boolean }) => cx('rounded-lg px-3 py-2 text-sm font-medium transition', isActive ? 'text-brand-700' : 'text-ink-600 hover:text-ink-900')
  return (
    <header className="sticky top-0 z-30 border-b border-ink-200/70 bg-white/90 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4">
        <div className="flex items-center gap-6">
          <Logo />
          <nav className="hidden items-center gap-1 md:flex">
            {navLinks.map((l) => (
              <NavLink key={l.to} to={l.to} className={linkCls}>
                {l.label}
              </NavLink>
            ))}
          </nav>
        </div>
        <div className="flex items-center gap-1">
          {user ? (
            <>
              <NavLink to="/dashboard" className={(s) => cx(linkCls(s), 'hidden sm:block')}>
                Dashboard
              </NavLink>
              <MessagesLink />
              <NotificationBell />
              <UserMenu />
            </>
          ) : (
            <div className="hidden items-center gap-2 sm:flex">
              <ButtonLink to="/login" variant="ghost">
                Log in
              </ButtonLink>
              <ButtonLink to="/signup">Join free</ButtonLink>
            </div>
          )}
          <button className="rounded-lg p-2 text-ink-600 hover:bg-ink-100 md:hidden" onClick={() => setMobile((m) => !m)} aria-label="Menu" aria-expanded={mobile}>
            {mobile ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>
        </div>
      </div>
      {mobile && (
        <nav className="border-t border-ink-100 bg-white px-4 py-3 md:hidden">
          {[...navLinks, ...(user ? [{ to: '/dashboard', label: 'Dashboard' }, { to: '/profile', label: 'My profile' }, { to: '/messages', label: 'Messages' }] : [{ to: '/login', label: 'Log in' }, { to: '/signup', label: 'Join free' }])].map((l) => (
            <NavLink key={l.to} to={l.to} className="block rounded-lg px-3 py-2.5 text-sm font-medium text-ink-700 hover:bg-ink-50">
              {l.label}
            </NavLink>
          ))}
        </nav>
      )}
    </header>
  )
}

function Footer() {
  return (
    <footer className="mt-16 border-t border-ink-200 bg-white">
      <div className="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:grid-cols-2 md:grid-cols-4">
        <div className="sm:col-span-2">
          <Logo />
          <p className="mt-3 max-w-sm text-sm text-ink-600">A companion for student life: learn from verified seniors on your campus, or earn by helping others. On campuses around the world.</p>
        </div>
        <div>
          <h3 className="text-sm font-semibold text-ink-900">Explore</h3>
          <ul className="mt-3 space-y-2 text-sm text-ink-600">
            <li><Link to="/tutors" className="hover:text-ink-900">Tutors</Link></li>
            <li><Link to="/guides" className="hover:text-ink-900">Campus guides</Link></li>
            <li><Link to="/prep" className="hover:text-ink-900">Placement prep</Link></li>
            <li><Link to="/feed" className="hover:text-ink-900">Campus feed</Link></li>
            <li><Link to="/campuses" className="hover:text-ink-900">Campuses</Link></li>
          </ul>
        </div>
        <div>
          <h3 className="text-sm font-semibold text-ink-900">Your Mate</h3>
          <ul className="mt-3 space-y-2 text-sm text-ink-600">
            <li><Link to="/about" className="hover:text-ink-900">About us</Link></li>
            <li><Link to="/contact" className="hover:text-ink-900">Contact</Link></li>
            <li><Link to="/campuses/request" className="hover:text-ink-900">Add your campus</Link></li>
          </ul>
        </div>
      </div>
      <p className="border-t border-ink-100 py-4 text-center text-xs text-ink-500">© {new Date().getFullYear()} Your Mate. Payments for sessions are arranged directly between students.</p>
    </footer>
  )
}

export function Layout() {
  return (
    <div className="flex min-h-screen flex-col">
      <Nav />
      <main className="flex-1">
        <Suspense fallback={<PageLoader />}>
          <Outlet />
        </Suspense>
      </main>
      <Footer />
    </div>
  )
}

export function Container({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx('mx-auto w-full max-w-6xl px-4 py-8', className)}>{children}</div>
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, booting } = useAuth()
  const location = useLocation()
  if (booting) return <PageLoader />
  if (!user) return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />
  return <>{children}</>
}

export function RequireAdmin({ children }: { children: ReactNode }) {
  const { user, booting } = useAuth()
  if (booting) return <PageLoader />
  if (!user) return <Navigate to="/login?next=/admin" replace />
  if (!user.isAdmin) return <Navigate to="/dashboard" replace />
  return <>{children}</>
}
