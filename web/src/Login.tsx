import { useEffect, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Building2, KeyRound, Lock, PlayCircle, ShieldCheck, User } from 'lucide-react'
import { authApi, type Me, type Providers } from './api'

const ssoMessages: Record<string, string> = {
  failed: 'Single sign-on did not complete. Try again, or ask your administrator to check the identity provider.',
  unknown: 'Your identity provider signed you in, but there is no prova account for you yet. Ask an administrator to add one.',
}

function Logo() {
  return (
    <svg viewBox="0 0 32 32" className="h-11 w-11" aria-hidden="true">
      <rect width="32" height="32" rx="8" fill="#0f172a" stroke="#1f2a44" />
      <path d="M10 25V7h7a5 5 0 0 1 0 10h-3.5" fill="none" stroke="#818cf8" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M16.5 22l2.4 2.4 5-5.4" fill="none" stroke="#22d3ee" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/** Login is the sign-in screen: a local account form, plus a button per configured identity provider. */
export function Login({ onSignedIn }: { onSignedIn: (me: Me) => void }) {
  const [params] = useSearchParams()
  const [providers, setProviders] = useState<Providers>({ local: true })
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | undefined>(ssoMessages[params.get('sso') ?? ''])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    authApi.providers().then(setProviders).catch(() => {})
  }, [])

  const signIn = async (n: string, pw: string) => {
    setBusy(true)
    setError(undefined)
    try {
      onSignedIn(await authApi.login(n, pw))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      onSignedIn(await authApi.login(name, password))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPassword('')
    } finally {
      setBusy(false)
    }
  }

  const sso = [providers.oidc, providers.saml].filter(Boolean) as { label: string; start: string }[]

  return (
    <main className="flex min-h-full items-center justify-center px-6 py-12">
      <div className="w-full max-w-[420px]">
        <div className="mb-8 flex flex-col items-center text-center">
          <Logo />
          <h1 className="mt-4 text-xl font-semibold tracking-tight text-text">Sign in to prova</h1>
          <p className="mt-1 text-sm text-muted">Financial protocol test &amp; proof harness</p>
        </div>

        <div className="rounded-[var(--radius)] border border-line bg-surface/70 p-6 shadow-[0_1px_0_0_rgba(255,255,255,0.03)_inset,0_24px_60px_-30px_rgba(0,0,0,0.9)] backdrop-blur-sm">
          {error && (
            <div role="alert" className="mb-4 rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">
              {error}
            </div>
          )}

          {providers.demo && (
            <div role="note" className="mb-5 rounded-lg border border-accent2/30 bg-accent2/10 p-4 text-sm">
              <div className="font-medium text-text">Try prova with the demonstration account</div>
              <p className="mt-1 text-muted">
                Name <code className="font-mono text-accent2">{providers.demo.name}</code>, password{' '}
                <code className="font-mono text-accent2">{providers.demo.password}</code>. It can look at every screen and
                change nothing. Simulated targets and invented data only.
              </p>
              <button
                type="button"
                disabled={busy}
                onClick={() => signIn(providers.demo!.name, providers.demo!.password)}
                className="mt-3 flex w-full items-center justify-center gap-2 rounded-lg bg-accent2/90 px-4 py-2 text-sm font-medium text-bg transition-colors hover:bg-accent2 disabled:opacity-60"
              >
                <PlayCircle className="h-4 w-4" aria-hidden="true" /> Sign in as the demonstration user
              </button>
            </div>
          )}

          {sso.length > 0 && (
            <>
              <div className="flex flex-col gap-2">
                {sso.map((p) => (
                  <a
                    key={p.start}
                    href={p.start}
                    className="flex items-center justify-center gap-2 rounded-lg border border-line bg-surface2 px-4 py-2.5 text-sm font-medium text-text transition-colors hover:border-accent/50 hover:bg-accent/10"
                  >
                    <Building2 className="h-4 w-4 text-accent2" aria-hidden="true" />
                    {p.label}
                  </a>
                ))}
              </div>
              <div className="my-5 flex items-center gap-3 text-[11px] uppercase tracking-wider text-faint">
                <span className="h-px flex-1 bg-line" />
                or with a prova account
                <span className="h-px flex-1 bg-line" />
              </div>
            </>
          )}

          <form onSubmit={submit} className="flex flex-col gap-4">
            <label className="flex flex-col gap-1.5">
              <span className="text-xs font-medium uppercase tracking-wider text-faint">Account</span>
              <span className="flex items-center gap-2 rounded-lg border border-line bg-bg/60 px-3 focus-within:border-accent/60 focus-within:ring-1 focus-within:ring-accent/30">
                <User className="h-4 w-4 text-faint" aria-hidden="true" />
                <input
                  name="username"
                  autoComplete="username"
                  autoFocus
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full bg-transparent py-2.5 text-sm text-text outline-none placeholder:text-faint"
                  placeholder="admin"
                />
              </span>
            </label>
            <label className="flex flex-col gap-1.5">
              <span className="text-xs font-medium uppercase tracking-wider text-faint">Password</span>
              <span className="flex items-center gap-2 rounded-lg border border-line bg-bg/60 px-3 focus-within:border-accent/60 focus-within:ring-1 focus-within:ring-accent/30">
                <KeyRound className="h-4 w-4 text-faint" aria-hidden="true" />
                <input
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full bg-transparent py-2.5 font-mono text-sm text-text outline-none"
                />
              </span>
            </label>
            <button
              type="submit"
              disabled={busy}
              className="mt-1 flex items-center justify-center gap-2 rounded-lg bg-accent px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-accent/90 disabled:opacity-60"
            >
              <Lock className="h-4 w-4" aria-hidden="true" />
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        </div>

        <p className="mt-6 flex items-center justify-center gap-1.5 text-xs text-faint">
          <ShieldCheck className="h-3.5 w-3.5 text-pass" aria-hidden="true" />
          Encrypted connection · sessions end after 30 minutes idle
        </p>
      </div>
    </main>
  )
}
