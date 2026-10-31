import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { BookMarked, Download, Loader2, Search, Send, ShieldCheck, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { Page, Card, Pill, Outcome } from './ui'
import { api, useAsync, type CatalogueMessage, type Run, type ValidationReport } from './api'
import { useCanRun } from './session'
import { ReadOnly } from './ReadOnly'

/** Catalogue browses every message in the ISO 20022 catalogue: a sample or a broken variant of any version,
 *  its official schema, a schema check, and a run that sends it to a gateway. */
export function Catalogue() {
  const navigate = useNavigate()
  const canRun = useCanRun()
  const { data: cat, error } = useAsync(() => api.catalogue(), [])
  const { data: targets = [] } = useAsync(() => api.targets(), [])
  const isoTargets = targets.filter((t) => t.protocol === 'iso20022')

  const [area, setArea] = useState('all')
  const [query, setQuery] = useState('')
  const [base, setBase] = useState('pacs.008')
  const [kind, setKind] = useState('')
  const [variant, setVariant] = useState('minimal')
  const [xml, setXml] = useState('')
  const [loading, setLoading] = useState(false)
  const [report, setReport] = useState<ValidationReport>()
  const [target, setTarget] = useState('')
  const [run, setRun] = useState<Run>()
  const [running, setRunning] = useState(false)

  const messages = cat?.messages ?? []
  const latest = useMemo(() => messages.filter((m) => m.latest), [messages])
  const versionsOf = useMemo(() => {
    const out: Record<string, CatalogueMessage[]> = {}
    for (const m of messages) (out[m.base] ??= []).push(m)
    // the main variant (001) first, newest version first; other variants after it
    const order = (m: CatalogueMessage) => `${999 - Number(m.kind.split('.')[2])}.${m.version}`
    for (const k of Object.keys(out)) out[k].sort((a, b) => order(b).localeCompare(order(a)))
    return out
  }, [messages])
  const areaCounts = useMemo(() => {
    const c: Record<string, number> = {}
    for (const m of latest) c[m.area] = (c[m.area] ?? 0) + 1
    return c
  }, [latest])
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return latest.filter((m) => (area === 'all' || m.area === area) &&
      (!q || `${m.base} ${m.name} ${m.root} ${m.kind}`.toLowerCase().includes(q)))
  }, [latest, area, query])
  const versions = versionsOf[base] ?? []
  const selected = versions.find((m) => m.kind === kind) ?? versions.find((m) => m.latest) ?? versions[0]

  useEffect(() => { if (!target && isoTargets.length) setTarget(isoTargets[0].name) }, [isoTargets, target])

  // Load the chosen variant of the chosen version whenever either changes.
  useEffect(() => {
    if (!selected) return
    let live = true
    setLoading(true)
    setReport(undefined)
    setRun(undefined)
    api.catalogueSample(selected.kind, variant)
      .then((x) => { if (live) setXml(x) })
      .catch((e) => { if (live) { setXml(''); toast.error('No such variant', { description: String((e as Error).message) }) } })
      .finally(() => live && setLoading(false))
    return () => { live = false }
  }, [selected?.kind, variant])

  const check = async () => {
    try { setReport(await api.validate(xml, '')) } catch (e) { toast.error('Validation failed', { description: String((e as Error).message) }) }
  }
  const send = async () => {
    if (!selected || !target) return
    setRunning(true)
    try {
      const r = await api.catalogueRun(selected.kind, variant, target)
      setRun(r)
      toast[r.outcome === 'pass' ? 'success' : r.outcome === 'fail' ? 'error' : 'warning'](`Verdict: ${r.outcome}`, { description: `${selected.kind} → ${target}` })
    } catch (e) {
      toast.error('Run failed', { description: String((e as Error).message) })
    } finally {
      setRunning(false)
    }
  }
  const findings = report?.findings ?? []
  const errors = findings.filter((f) => f.severity === 'error').length
  const variantInfo = cat?.variants.find((v) => v.id === variant)
  const broken = variant !== 'minimal' && variant !== 'full' && variant !== 'space'

  return (
    <Page
      title="Catalogue"
      subtitle={`Every message in the ISO 20022 catalogue — ${latest.length || 707} messages, ${messages.length || 2641} versions, ${Object.keys(cat?.areas ?? {}).length || 31} business areas — with a valid sample or a broken variant of each, its official schema, and a gateway to send it to`}
      actions={<Pill>libxml2-checked</Pill>}
    >
      {error && <p className="mb-4 text-sm text-fail">{error}</p>}
      <div className="grid h-full grid-cols-[210px_320px_minmax(0,1fr)] gap-4">
        <Card className="flex min-h-0 flex-col overflow-hidden p-0">
          <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">Business areas</div>
          <div className="min-h-0 flex-1 overflow-auto py-1" role="listbox" aria-label="Business area">
            {[['all', 'All areas'] as [string, string], ...Object.entries(cat?.areas ?? {}).sort()].map(([code, name]) => (
              <button key={code} role="option" aria-selected={area === code} onClick={() => setArea(code)}
                className={`flex w-full items-center justify-between gap-2 px-4 py-1.5 text-left text-xs transition-colors hover:bg-surface2/50 ${area === code ? 'bg-accent/15 text-text' : 'text-muted'}`}>
                <span className="min-w-0 truncate">{code !== 'all' && <span className="mr-1.5 font-mono text-accent">{code}</span>}{name}</span>
                <span className="tnum text-faint">{code === 'all' ? latest.length : areaCounts[code] ?? 0}</span>
              </button>
            ))}
          </div>
        </Card>

        <Card className="flex min-h-0 flex-col overflow-hidden p-0">
          <label className="m-3 flex items-center gap-2 rounded-md border border-line bg-bg/40 px-2.5 py-1.5">
            <Search className="h-3.5 w-3.5 text-faint" aria-hidden />
            <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search messages (sese.023, statement …)"
              aria-label="Search messages" className="w-full bg-transparent text-sm text-text outline-none placeholder:text-faint" />
          </label>
          <div className="min-h-0 flex-1 overflow-auto border-t border-line">
            {shown.length === 0 && <p className="px-4 py-6 text-sm text-faint">No message matches.</p>}
            {shown.map((m) => (
              <button key={m.base} onClick={() => { setBase(m.base); setKind('') }}
                className={`flex w-full items-start gap-3 border-b border-line/50 px-4 py-2.5 text-left transition-colors hover:bg-surface2/50 ${m.base === base ? 'bg-surface2/60' : ''}`}>
                <span className="w-16 shrink-0 font-mono text-xs text-accent">{m.base}</span>
                <span className="min-w-0">
                  <span className="block truncate text-sm text-text">{m.name}</span>
                  <span className="block text-[11px] text-faint">{(versionsOf[m.base] ?? []).length} versions · latest {m.kind.slice(m.base.length + 1)}{m.spoken ? ' · spoken natively' : ''}</span>
                </span>
              </button>
            ))}
          </div>
        </Card>

        <div className="flex min-h-0 min-w-0 flex-col gap-4">
          <Card className="p-4">
            <div className="flex flex-wrap items-center gap-2">
              <BookMarked className="h-4 w-4 text-accent" aria-hidden />
              <span className="text-sm font-medium text-text">{selected?.name ?? '—'}</span>
              <span className="font-mono text-xs text-muted">{selected?.root}</span>
              <span className="ml-auto" />
              <select aria-label="Version" value={selected?.kind ?? ''} onChange={(e) => setKind(e.target.value)}
                className="rounded-md border border-line bg-surface2 px-2 py-1 font-mono text-xs text-text">
                {versions.map((v) => <option key={v.kind} value={v.kind}>{v.kind}{v.latest ? ' (latest)' : ''}</option>)}
              </select>
              <select aria-label="Instance" value={variant} onChange={(e) => setVariant(e.target.value)}
                className="max-w-[16rem] rounded-md border border-line bg-surface2 px-2 py-1 text-xs text-text">
                <optgroup label="Valid">
                  {(cat?.variants ?? []).filter((v) => ['minimal', 'full', 'space'].includes(v.id)).map((v) => <option key={v.id} value={v.id}>{v.id}: {v.description}</option>)}
                </optgroup>
                <optgroup label="Broken">
                  {(cat?.variants ?? []).filter((v) => !['minimal', 'full', 'space'].includes(v.id)).map((v) => <option key={v.id} value={v.id}>{v.id}: {v.description}</option>)}
                </optgroup>
              </select>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted">
              <span>{selected?.namespace}</span>
              {variantInfo && <Pill>{broken ? 'broken: ' : ''}{variantInfo.description}</Pill>}
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <button onClick={check} disabled={!xml} className="flex items-center gap-1.5 rounded-lg border border-line bg-surface2 px-3 py-1.5 text-xs text-text hover:bg-surface2/70 disabled:opacity-50">
                <ShieldCheck className="h-3.5 w-3.5 text-accent" /> Check against the schema
              </button>
              <button onClick={() => navigate('/validate', { state: { xml } })} disabled={!xml}
                className="rounded-lg border border-line bg-surface2 px-3 py-1.5 text-xs text-muted hover:text-text disabled:opacity-50">Open in Validate</button>
              <button onClick={() => { navigator.clipboard?.writeText(xml); toast.success('Copied') }} disabled={!xml}
                className="flex items-center gap-1.5 rounded-lg border border-line bg-surface2 px-3 py-1.5 text-xs text-muted hover:text-text disabled:opacity-50">
                <Copy className="h-3.5 w-3.5" /> Copy</button>
              {selected && <a href={`/api/catalogue/${selected.kind}/schema`} className="flex items-center gap-1.5 rounded-lg border border-line bg-surface2 px-3 py-1.5 text-xs text-muted hover:text-text">
                <Download className="h-3.5 w-3.5" /> Schema</a>}
              <span className="ml-auto flex flex-wrap items-center gap-2">
                <select aria-label="Target" value={target} onChange={(e) => setTarget(e.target.value)}
                  className="rounded-md border border-line bg-surface2 px-2 py-1 text-xs text-text">
                  {isoTargets.map((t) => <option key={t.name} value={t.name}>{t.name}</option>)}
                </select>
                {canRun ? (
                  <button onClick={send} disabled={running || !selected}
                    className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/90 disabled:opacity-50">
                    {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />} Send
                  </button>
                ) : <ReadOnly what="send messages" />}
              </span>
            </div>
            {report && (
              <div className={`mt-3 rounded-lg border px-3 py-2 text-xs ${errors ? 'border-fail/30 bg-fail/10 text-fail' : 'border-pass/30 bg-pass/10 text-pass'}`}>
                {errors ? `${errors} schema error${errors > 1 ? 's' : ''}: ${findings.find((f) => f.severity === 'error')?.message} at ${findings.find((f) => f.severity === 'error')?.path}` : `Valid against ${report.kind}'s official schema`}
              </div>
            )}
            {run && (
              <div className="mt-3 flex flex-wrap items-center gap-3 rounded-lg border border-line bg-surface2/50 px-3 py-2 text-xs">
                <Outcome value={run.outcome} />
                <span className="text-muted">{run.messages[1]?.kind}: {run.messages[1]?.fields.decision}{run.messages[1]?.fields.RsnDesc ? ` — ${run.messages[1].fields.RsnDesc}` : ''}</span>
                <button onClick={() => navigate(`/proof?run=${run.id}`)} className="ml-auto text-accent hover:underline">Open the proof</button>
              </div>
            )}
          </Card>
          <Card className="min-h-0 flex-1 overflow-hidden p-0">
            <div className="flex items-center justify-between border-b border-line px-4 py-2 text-xs text-faint">
              <span>{selected?.kind} · {variant}</span>
              <span className="tnum">{xml ? `${xml.split('\n').length} lines` : ''}</span>
            </div>
            {loading ? <p className="flex items-center gap-2 px-4 py-6 text-sm text-faint"><Loader2 className="h-4 w-4 animate-spin" /> Building the instance…</p> : (
              <pre aria-label="Message" className="h-full overflow-auto px-4 py-3 font-mono text-[12px] leading-relaxed text-text">{xml}</pre>
            )}
          </Card>
        </div>
      </div>
    </Page>
  )
}
