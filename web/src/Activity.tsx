import { useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { Page, Card, Pill } from './ui'
import { authApi, useAsync } from './api'

/** Activity is the activity log: who signed in, who changed an account, who ran what. Administrators only. */
export function Activity() {
  const { data, error } = useAsync(() => authApi.activity(), [])
  const [q, setQ] = useState('')
  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (data ?? []).filter((a) => !needle || `${a.actor} ${a.action} ${a.detail ?? ''}`.toLowerCase().includes(needle))
  }, [data, q])

  return (
    <Page
      title="Activity"
      subtitle="Who signed in, who changed an account, and who ran what"
      actions={<Pill>{rows.length} entries</Pill>}
    >
      <Card className="overflow-hidden">
        <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
          <Search className="h-4 w-4 text-faint" aria-hidden="true" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            aria-label="Filter the activity log"
            placeholder="Filter by person, action or detail…"
            className="w-full bg-transparent py-1 text-sm text-text outline-none placeholder:text-faint"
          />
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-[11px] uppercase tracking-wider text-faint">
              <th className="px-5 py-2 font-medium">When</th>
              <th className="px-3 py-2 font-medium">Who</th>
              <th className="px-3 py-2 font-medium">What</th>
              <th className="px-5 py-2 font-medium">Detail</th>
            </tr>
          </thead>
          <tbody>
            {error && <tr><td colSpan={4} className="px-5 py-4 text-fail">{error}</td></tr>}
            {!error && rows.length === 0 && (
              <tr><td colSpan={4} className="px-5 py-4 text-muted">Nothing recorded yet.</td></tr>
            )}
            {rows.map((a, i) => (
              <tr key={i} className="border-t border-line/60 hover:bg-surface2/40">
                <td className="whitespace-nowrap px-5 py-2 tnum text-muted">{new Date(a.at).toLocaleString()}</td>
                <td className="px-3 py-2 font-mono text-xs text-text">{a.actor}</td>
                <td className="px-3 py-2 text-text">{a.action}</td>
                <td className="px-5 py-2 text-muted">{a.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </Page>
  )
}
