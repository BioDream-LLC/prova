import { useEffect, useMemo, useRef, useState } from 'react'
import { basicSetup, EditorView } from 'codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { oneDark } from '@codemirror/theme-one-dark'
import { Play, Loader2, Search } from 'lucide-react'
import { toast } from 'sonner'
import { useNavigate } from 'react-router-dom'
import { Page, Pill } from './ui'
import { api, useAsync, type Scenario } from './api'
import { useCanRun } from './session'
import { ReadOnly } from './ReadOnly'

/** Render a scenario as the declarative casebook form prova runs (and the DSL goja will author). */
function preview(s: Scenario): string {
  const obj = (o: Record<string, string>, indent: string) => {
    const keys = Object.keys(o)
    if (keys.length === 0) return '{}'
    return '{\n' + keys.map((k) => `${indent}  ${k}: ${JSON.stringify(o[k])},`).join('\n') + `\n${indent}}`
  }
  return (
    `scenario(${JSON.stringify(s.name)}, {\n` +
    `  protocol: ${JSON.stringify(s.protocol)},\n` +
    (s.message ? `  message: ${JSON.stringify(s.message)},\n` : '') +
    `  request: ${obj(s.request, '  ')},\n` +
    `  context: ${obj(s.context, '  ')},\n` +
    `  expect: ${JSON.stringify(s.expect)},\n` +
    `})\n`
  )
}

export function Scenarios() {
  const navigate = useNavigate()
  const { data: scenarios = [] } = useAsync(() => api.scenarios(), [])
  const { data: targets = [] } = useAsync(() => api.targets(), [])

  const [selectedId, setSelectedId] = useState<string>()
  const [target, setTarget] = useState<string>('')
  const [running, setRunning] = useState(false)
  const [protocol, setProtocol] = useState<string>('all')
  const [query, setQuery] = useState('')

  const canRun = useCanRun()
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return scenarios.filter(
      (s) =>
        (protocol === 'all' || s.protocol === protocol) &&
        (!q || `${s.name} ${s.id} ${s.expect} ${s.description}`.toLowerCase().includes(q)),
    )
  }, [scenarios, protocol, query])
  const selected = useMemo(
    () => scenarios.find((s) => s.id === selectedId) ?? shown[0] ?? scenarios[0],
    [scenarios, shown, selectedId],
  )
  // Offer only the targets that speak the selected scenario's protocol, compliant first.
  const usable = useMemo(
    () => targets.filter((t) => !selected || t.protocol === selected.protocol),
    [targets, selected],
  )
  useEffect(() => {
    if (usable.length && !usable.some((t) => t.name === target)) setTarget(usable[0].name)
  }, [usable, target])
  const counts = useMemo(() => {
    const c: Record<string, number> = { all: scenarios.length }
    for (const s of scenarios) c[s.protocol] = (c[s.protocol] ?? 0) + 1
    return c
  }, [scenarios])

  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  useEffect(() => {
    const el = editorRef.current
    if (!el || !selected) return
    const view = new EditorView({
      doc: preview(selected),
      extensions: [
        basicSetup,
        javascript(),
        oneDark,
        EditorView.theme({
          '&': { height: '100%', backgroundColor: 'transparent', fontSize: '13px' },
          '.cm-gutters': { backgroundColor: 'transparent', borderRight: '1px solid #1f2a44', color: '#5b6880' },
          '.cm-content': { fontFamily: 'JetBrains Mono, ui-monospace, monospace' },
          '.cm-activeLine': { backgroundColor: 'rgba(99,102,241,0.06)' },
          '.cm-activeLineGutter': { backgroundColor: 'transparent' },
        }),
      ],
      parent: el,
    })
    viewRef.current = view
    return () => { view.destroy(); viewRef.current = null }
  }, [selected])

  async function run() {
    if (!target) return
    const src = viewRef.current?.state.doc.toString() ?? ''
    if (!src.trim()) return
    setRunning(true)
    try {
      const r = await api.runScript(src, target)
      toast[r.outcome === 'pass' ? 'success' : r.outcome === 'fail' ? 'error' : 'warning'](
        `Verdict: ${r.outcome}`,
        { description: `${r.scenario} → ${target}` },
      )
      navigate(`/proof?run=${r.id}`)
    } catch (e) {
      toast.error('Run failed', { description: String((e as Error).message) })
    } finally {
      setRunning(false)
    }
  }

  return (
    <Page
      title="Scenarios"
      subtitle="Drive a scripted exchange at a target; the verdict is proved, not asserted"
      actions={
        <div className="flex items-center gap-2">
          <select
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            className="rounded-lg border border-line bg-surface/60 px-2.5 py-1.5 text-sm text-text outline-none"
          >
            {usable.map((t) => (
              <option key={t.name} value={t.name}>{t.name}{t.note ? ` — ${t.note}` : ''}</option>
            ))}
          </select>
          {canRun ? (
          <button
              onClick={run}
              disabled={running || !selected}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white shadow-sm transition-colors hover:bg-accent/90 disabled:opacity-50"
            >
              {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Run
            </button>
          ) : <ReadOnly what="run scenarios" />}
        </div>
      }
    >
      <div className="grid h-full grid-cols-[340px_1fr] gap-4">
        <div className="flex min-h-0 flex-col overflow-hidden rounded-[var(--radius)] border border-line bg-surface/40">
          <div className="space-y-2.5 border-b border-line px-3 py-3">
            <div className="flex gap-1" role="tablist" aria-label="Protocol">
              {[['all', 'All'], ['iso20022', 'ISO 20022'], ['iso8583', 'ISO 8583'], ['fix', 'FIX']].map(([k, label]) => (
                <button
                  key={k}
                  role="tab"
                  aria-selected={protocol === k}
                  onClick={() => setProtocol(k)}
                  className={`rounded-md px-2 py-1 text-xs transition-colors ${protocol === k ? 'bg-accent/20 text-text' : 'text-muted hover:bg-surface2/60'}`}
                >
                  {label} <span className="text-faint">{counts[k] ?? 0}</span>
                </button>
              ))}
            </div>
            <label className="flex items-center gap-2 rounded-md border border-line bg-bg/40 px-2.5 py-1.5">
              <Search className="h-3.5 w-3.5 text-faint" aria-hidden />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search scenarios"
                aria-label="Search scenarios"
                className="w-full bg-transparent text-sm text-text outline-none placeholder:text-faint"
              />
            </label>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
          {shown.length === 0 && <p className="px-4 py-6 text-sm text-faint">No scenario matches.</p>}
          {shown.map((s) => (
            <button
              key={s.id}
              onClick={() => setSelectedId(s.id)}
              className={`flex w-full items-center justify-between gap-2 border-b border-line/50 px-4 py-3 text-left text-sm transition-colors hover:bg-surface2/50 ${s.id === selected?.id ? 'bg-surface2/60' : ''}`}
            >
              <span className="min-w-0">
                <span className="block truncate text-text">{s.name}</span>
                <span className="block truncate text-[11px] text-faint">expect {s.expect}</span>
              </span>
            </button>
          ))}
          </div>
        </div>

        <div className="flex min-h-0 flex-col overflow-hidden rounded-[var(--radius)] border border-line bg-surface/40">
          <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
            <span className="font-mono text-xs text-muted">{selected?.name}</span>
            <span className="flex gap-1.5">
              {selected?.message && <Pill>{selected.message}</Pill>}
              <Pill>{selected?.protocol}</Pill>
            </span>
          </div>
          {selected?.description && (
            <p className="border-b border-line/60 px-4 py-2.5 text-sm text-muted">{selected.description}</p>
          )}
          <div ref={editorRef} className="min-h-0 flex-1 overflow-auto" />
        </div>
      </div>
    </Page>
  )
}
