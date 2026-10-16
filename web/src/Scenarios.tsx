import { useEffect, useMemo, useRef, useState } from 'react'
import { basicSetup, EditorView } from 'codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { oneDark } from '@codemirror/theme-one-dark'
import { Play, Loader2 } from 'lucide-react'
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

  const canRun = useCanRun()
  const selected = useMemo(
    () => scenarios.find((s) => s.id === selectedId) ?? scenarios[0],
    [scenarios, selectedId],
  )
  useEffect(() => {
    if (!target && targets.length) setTarget(targets[0].name)
  }, [targets, target])

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
            {targets.map((t) => (
              <option key={t.name} value={t.name}>{t.name}</option>
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
      <div className="grid h-full grid-cols-[300px_1fr] gap-4">
        <div className="overflow-auto rounded-[var(--radius)] border border-line bg-surface/40">
          <div className="border-b border-line px-4 py-3 text-xs font-medium uppercase tracking-wider text-faint">Scenarios</div>
          {scenarios.map((s) => (
            <button
              key={s.id}
              onClick={() => setSelectedId(s.id)}
              className={`flex w-full items-center justify-between gap-2 border-b border-line/50 px-4 py-3 text-left text-sm transition-colors hover:bg-surface2/50 ${s.id === selected?.id ? 'bg-surface2/60' : ''}`}
            >
              <span className="min-w-0">
                <span className="block truncate text-text">{s.name}</span>
                <span className="text-[11px] uppercase text-faint">{s.protocol} · expect {s.expect}</span>
              </span>
            </button>
          ))}
        </div>

        <div className="flex min-h-0 flex-col overflow-hidden rounded-[var(--radius)] border border-line bg-surface/40">
          <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
            <span className="font-mono text-xs text-muted">{selected?.name}</span>
            <Pill>{selected?.protocol}</Pill>
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
