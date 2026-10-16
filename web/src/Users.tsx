import { useState, type FormEvent } from 'react'
import { Copy, Trash2, UserPlus } from 'lucide-react'
import { toast } from 'sonner'
import { Page, Card, Pill } from './ui'
import { authApi, useAsync, type Role } from './api'
import { useMe } from './session'

const roleTone: Record<Role, string> = {
  admin: 'text-accent ring-accent/30 bg-accent/10',
  operator: 'text-accent2 ring-accent2/30 bg-accent2/10',
  viewer: 'text-muted ring-line bg-surface2',
}

/** Users is account management for administrators. */
export function Users() {
  const me = useMe()
  const { data: users, reload, error } = useAsync(() => authApi.users(), [])
  const [name, setName] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [sso, setSso] = useState(false)
  const [issued, setIssued] = useState<{ name: string; password: string }>()

  const add = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const res = await authApi.setUser(name.trim().toLowerCase(), role, sso)
      if (res.password) setIssued({ name: res.name, password: res.password })
      else toast.success(`${res.name} can now sign in through single sign-on`)
      setName('')
      reload()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const remove = async (n: string) => {
    if (!window.confirm(`Remove the account ${n}? Their sessions end at once.`)) return
    try {
      await authApi.removeUser(n)
      toast.success(`removed ${n}`)
      reload()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const reset = async (n: string, r: Role) => {
    try {
      const res = await authApi.setUser(n, r, false)
      if (res.password) setIssued({ name: n, password: res.password })
      reload()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Page title="Users" subtitle="Who may use prova, and what they may do" actions={<Pill>{users?.length ?? 0} accounts</Pill>}>
      <div className="grid grid-cols-[1fr_360px] gap-4">
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-xs uppercase tracking-wider text-faint">
                <th className="px-4 py-3 font-medium">Account</th>
                <th className="px-4 py-3 font-medium">Role</th>
                <th className="px-4 py-3 font-medium">Signs in with</th>
                <th className="px-4 py-3 font-medium">Created</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {error && (
                <tr><td colSpan={5} className="px-4 py-6 text-fail">{error}</td></tr>
              )}
              {(users ?? []).map((u) => (
                <tr key={u.name} className="border-b border-line/60 last:border-0 hover:bg-surface2/50">
                  <td className="px-4 py-3 font-mono text-text">
                    {u.name}
                    {u.name === me?.user && <span className="ml-2 text-[10px] uppercase tracking-wider text-faint">you</span>}
                  </td>
                  <td className="px-4 py-3">
                    <span className={`rounded-full px-2 py-0.5 text-xs ring-1 ${roleTone[u.role]}`}>{u.role}</span>
                  </td>
                  <td className="px-4 py-3 text-muted">{u.sso ? 'single sign-on' : 'password'}</td>
                  <td className="px-4 py-3 text-muted tnum">{u.created ? new Date(u.created).toLocaleDateString() : '—'}</td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex justify-end gap-1">
                      {!u.sso && (
                        <button
                          onClick={() => reset(u.name, u.role)}
                          className="rounded-md px-2 py-1 text-xs text-muted hover:bg-surface2 hover:text-text"
                        >
                          New password
                        </button>
                      )}
                      {u.name !== me?.user && (
                        <button
                          onClick={() => remove(u.name)}
                          aria-label={`Remove ${u.name}`}
                          className="rounded-md p-1.5 text-faint hover:bg-fail/10 hover:text-fail"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>

        <div className="flex flex-col gap-4">
          <Card className="p-5">
            <div className="mb-3 flex items-center gap-2 text-sm font-medium text-text">
              <UserPlus className="h-4 w-4 text-accent" /> Add an account
            </div>
            <form onSubmit={add} className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-xs text-faint">
                Name
                <input
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  pattern="[a-z][a-z0-9._\-]{1,63}"
                  title="lower case letters, digits, '.', '_' or '-', starting with a letter"
                  className="rounded-lg border border-line bg-bg/60 px-3 py-2 font-mono text-sm text-text outline-none focus:border-accent/60"
                />
              </label>
              <label className="flex flex-col gap-1 text-xs text-faint">
                Role
                <select
                  value={role}
                  onChange={(e) => setRole(e.target.value as Role)}
                  className="rounded-lg border border-line bg-bg/60 px-3 py-2 text-sm text-text outline-none"
                >
                  <option value="viewer">viewer — reads runs and proofs</option>
                  <option value="operator">operator — also runs scenarios and suites</option>
                  <option value="admin">admin — also manages accounts</option>
                </select>
              </label>
              <label className="flex items-center gap-2 text-sm text-muted">
                <input type="checkbox" checked={sso} onChange={(e) => setSso(e.target.checked)} className="accent-indigo-500" />
                Signs in through single sign-on (no password)
              </label>
              <button type="submit" className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent/90">
                Add account
              </button>
            </form>
          </Card>

          {issued && (
            <Card className="border-indet/40 p-5">
              <div className="text-xs font-medium uppercase tracking-wider text-indet">Password for {issued.name}</div>
              <div className="mt-2 flex items-center gap-2">
                <code className="flex-1 rounded-md border border-line bg-bg/60 px-2 py-1.5 font-mono text-sm text-text">{issued.password}</code>
                <button
                  onClick={() => navigator.clipboard.writeText(issued.password).then(() => toast.success('copied'))}
                  aria-label="Copy password"
                  className="rounded-md p-2 text-muted hover:bg-surface2 hover:text-text"
                >
                  <Copy className="h-4 w-4" />
                </button>
              </div>
              <p className="mt-2 text-xs text-muted">Shown once. Pass it on securely; prova stores only its hash.</p>
            </Card>
          )}
        </div>
      </div>
    </Page>
  )
}
