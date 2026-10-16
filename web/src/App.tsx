import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import {
  Activity as ActivityIcon, BarChart3, BookOpen, ExternalLink, FileCode2, History, LayoutDashboard, Share2, Table2,
  ListChecks, ArrowRightLeft, LogOut, Users as UsersIcon,
} from 'lucide-react'
import { Dashboard } from './Dashboard'
import { Scenarios } from './Scenarios'
import { Suites } from './Suites'
import { Messages } from './Messages'
import { Proof } from './Proof'
import { Transform } from './Transform'
import { Reports } from './Reports'
import { Activity } from './Activity'
import { Login } from './Login'
import { Users } from './Users'
import { AUTH_EVENT, authApi, type Me } from './api'
import { MeContext } from './session'

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/scenarios', label: 'Scenarios', icon: FileCode2 },
  { to: '/suites', label: 'Suites', icon: ListChecks },
  { to: '/messages', label: 'Messages', icon: Table2 },
  { to: '/transform', label: 'Transform', icon: ArrowRightLeft },
  { to: '/proof', label: 'Proof', icon: Share2 },
  { to: '/reports', label: 'Reports', icon: BarChart3 },
]

const adminNav = [
  { to: '/users', label: 'Users', icon: UsersIcon },
  { to: '/activity', label: 'Activity', icon: History },
]

const linkClass = ({ isActive }: { isActive: boolean }) =>
  [
    'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
    isActive ? 'bg-accent/15 text-text ring-1 ring-accent/25' : 'text-muted hover:bg-surface2 hover:text-text',
  ].join(' ')

function Sidebar({ me, onSignOut }: { me: Me; onSignOut: () => void }) {
  return (
    <aside className="flex w-60 shrink-0 flex-col border-r border-line bg-surface/40">
      <div className="flex items-center gap-2.5 px-5 py-5">
        <Logo />
        <div className="leading-tight">
          <div className="text-sm font-semibold tracking-tight">prova</div>
          <div className="text-[11px] text-faint">proof-carrying test harness</div>
        </div>
      </div>
      <nav className="flex flex-col gap-0.5 px-3">
        {nav.map(({ to, label, icon: Icon, end }) => (
          <NavLink key={to} to={to} end={end} className={linkClass}>
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
        {me.role === 'admin' && (
          <>
            <div className="mt-4 px-3 pb-1 text-[10px] font-medium uppercase tracking-wider text-faint">Administer</div>
            {adminNav.map(({ to, label, icon: Icon }) => (
              <NavLink key={to} to={to} className={linkClass}>
                <Icon className="h-4 w-4" />
                {label}
              </NavLink>
            ))}
          </>
        )}
        {/* The manual ships in the binary and opens in its own tab, so it can stay open beside the screen it explains. */}
        <a
          href="/manual"
          target="_blank"
          rel="noreferrer"
          className="group mt-4 flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:bg-surface2 hover:text-text"
          title="The reference manual, served by this server"
        >
          <BookOpen className="h-4 w-4" />
          Manual
          <ExternalLink className="ml-auto h-3 w-3 opacity-60" aria-hidden="true" />
        </a>
      </nav>
      <div className="mt-auto flex flex-col gap-3 px-4 py-4">
        <div className="flex items-center gap-2.5 rounded-lg border border-line bg-surface2/60 px-3 py-2">
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent/20 text-xs font-semibold uppercase text-accent">
            {me.user.slice(0, 1)}
          </span>
          <div className="min-w-0 flex-1 leading-tight">
            <div className="truncate font-mono text-xs text-text">{me.user}</div>
            <div className="text-[10px] uppercase tracking-wider text-faint">
              {me.role} · {me.via === 'password' ? 'local' : me.via}
            </div>
          </div>
          <button
            onClick={onSignOut}
            aria-label="Sign out"
            title="Sign out"
            className="rounded-md p-1.5 text-faint hover:bg-surface hover:text-text"
          >
            <LogOut className="h-3.5 w-3.5" />
          </button>
        </div>
        <div className="flex items-center gap-1.5 px-1 text-[11px] text-faint">
          <span className="h-1.5 w-1.5 rounded-full bg-pass" />
          FIX · ISO 8583 · ISO 20022
        </div>
      </div>
    </aside>
  )
}

function Logo() {
  return (
    <svg viewBox="0 0 32 32" className="h-8 w-8">
      <rect width="32" height="32" rx="8" fill="#0f172a" stroke="#1f2a44" />
      <path d="M10 25V7h7a5 5 0 0 1 0 10h-3.5" fill="none" stroke="#818cf8" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M16.5 22l2.4 2.4 5-5.4" fill="none" stroke="#22d3ee" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Shell({ me, onSignOut, children }: { me: Me; onSignOut: () => void; children: ReactNode }) {
  return (
    <div className="flex h-screen w-screen overflow-hidden text-text">
      <Sidebar me={me} onSignOut={onSignOut} />
      <div className="flex min-w-0 flex-1 flex-col">
        {me.demo && (
          <div role="note" className="flex items-center justify-center gap-2 border-b border-accent2/20 bg-accent2/10 px-4 py-1.5 text-xs text-accent2">
            Public demonstration · simulated targets and invented data only · it can look at everything and change nothing
          </div>
        )}
        <main className="min-h-0 flex-1">{children}</main>
      </div>
    </div>
  )
}

export function App() {
  // undefined: still asking the server; null: signed out.
  const [me, setMe] = useState<Me | null | undefined>(undefined)
  const navigate = useNavigate()
  const location = useLocation()

  useEffect(() => {
    authApi.me().then(setMe).catch(() => setMe(null))
    const ended = () => setMe(null)
    window.addEventListener(AUTH_EVENT, ended)
    return () => window.removeEventListener(AUTH_EVENT, ended)
  }, [])

  const signOut = useCallback(async () => {
    await authApi.logout().catch(() => {})
    setMe(null)
    navigate('/login', { replace: true })
  }, [navigate])

  if (me === undefined) return null

  if (me === null) {
    return (
      <Routes>
        <Route
          path="/login"
          element={<Login onSignedIn={(m) => { setMe(m); navigate('/', { replace: true }) }} />}
        />
        <Route path="*" element={<Navigate to="/login" replace state={{ from: location.pathname }} />} />
      </Routes>
    )
  }

  return (
    <MeContext.Provider value={me}>
      <Shell me={me} onSignOut={signOut}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/scenarios" element={<Scenarios />} />
          <Route path="/suites" element={<Suites />} />
          <Route path="/messages" element={<Messages />} />
          <Route path="/transform" element={<Transform />} />
          <Route path="/proof" element={<Proof />} />
          <Route path="/reports" element={<Reports />} />
          <Route path="/users" element={me.role === 'admin' ? <Users /> : <Navigate to="/" replace />} />
          <Route path="/activity" element={me.role === 'admin' ? <Activity /> : <Navigate to="/" replace />} />
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </Shell>
    </MeContext.Provider>
  )
}

function NotFound() {
  return (
    <div className="flex h-full items-center justify-center text-muted">
      <ActivityIcon className="mr-2 h-4 w-4" /> Not found
    </div>
  )
}
