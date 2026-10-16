import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Loader2, Gauge } from 'lucide-react'
import { toast } from 'sonner'
import { Page, Card, Pill, Outcome, Stat, fmtMs } from './ui'
import { api, useAsync, type SuiteResult, type LoadResult } from './api'
import { useCanRun } from './session'
import { ReadOnly } from './ReadOnly'

export function Suites() {
  const canRun = useCanRun()
  const navigate = useNavigate()
  const { data: suites = [] } = useAsync(() => api.suites(), [])
  const { data: targets = [] } = useAsync(() => api.targets(), [])
  const { data: scenarios = [] } = useAsync(() => api.scenarios(), [])

  const [running, setRunning] = useState('')
  const [result, setResult] = useState<SuiteResult>()

  async function runSuite(id: string, target: string) {
    setRunning(id)
    try {
      const r = await api.runSuite(id, target)
      setResult(r)
      toast[r.outcome === 'pass' ? 'success' : r.outcome === 'fail' ? 'error' : 'warning'](
        `Suite ${r.outcome}`,
        { description: `${r.pass} pass · ${r.fail} fail · ${r.indeterminate} indeterminate` },
      )
    } catch (e) {
      toast.error('Suite failed', { description: String((e as Error).message) })
    } finally {
      setRunning('')
    }
  }

  return (
    <Page title="Suites" subtitle="Regression & certification packs, and load tests — proved, not just passed">
      <div className="grid grid-cols-2 gap-4">
        <div className="flex flex-col gap-3">
          {suites.map((s) => (
            <Card key={s.id} className="p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-text">{s.name}</div>
                  <p className="mt-0.5 text-xs text-muted">{s.description}</p>
                  <div className="mt-2 flex flex-wrap items-center gap-1.5">
                    <Pill>{s.target}</Pill>
                    <span className="text-xs text-faint">{s.scenarios.length} scenarios</span>
                  </div>
                </div>
                {canRun ? (
                <button
                    onClick={() => runSuite(s.id, s.target)}
                    disabled={running !== ''}
                    className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-accent/90 disabled:opacity-50"
                  >
                    {running === s.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Run
                  </button>
                ) : <ReadOnly what="run suites" />}
              </div>
            </Card>
          ))}

          <LoadPanel scenarios={scenarios.map((s) => ({ id: s.id, name: s.name }))} targets={targets.map((t) => t.name)} />
        </div>

        <div className="min-h-0">
          {result ? (
            <Card className="flex h-full flex-col">
              <div className="flex items-center justify-between border-b border-line px-5 py-3">
                <div>
                  <div className="text-sm font-medium text-text">{result.name}</div>
                  <div className="text-xs text-faint">{result.target} · {result.durationMs}ms</div>
                </div>
                <Outcome value={result.outcome} />
              </div>
              <div className="grid grid-cols-3 gap-px bg-line text-center">
                <div className="bg-surface px-3 py-3"><div className="tnum text-xl font-semibold text-pass">{result.pass}</div><div className="text-[11px] uppercase text-faint">pass</div></div>
                <div className="bg-surface px-3 py-3"><div className="tnum text-xl font-semibold text-fail">{result.fail}</div><div className="text-[11px] uppercase text-faint">fail</div></div>
                <div className="bg-surface px-3 py-3"><div className="tnum text-xl font-semibold text-indet">{result.indeterminate}</div><div className="text-[11px] uppercase text-faint">indeterminate</div></div>
              </div>
              <table className="w-full text-sm">
                <tbody>
                  {result.runs.map((r) => (
                    <tr key={r.id} onClick={() => navigate(`/proof?run=${r.id}`)} className="cursor-pointer border-t border-line/60 hover:bg-surface2/40">
                      <td className="whitespace-nowrap px-5 py-2.5 font-mono text-xs text-muted">{r.id}</td>
                      <td className="px-5 py-2.5 text-text">{r.scenario}</td>
                      <td className="px-5 py-2.5 text-right"><Outcome value={r.outcome} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          ) : (
            <Card className="flex h-full items-center justify-center p-8 text-sm text-faint">
              Run a suite to see its aggregate verdict and the proof behind each run.
            </Card>
          )}
        </div>
      </div>
    </Page>
  )
}

function LoadPanel({ scenarios, targets }: { scenarios: { id: string; name: string }[]; targets: string[] }) {
  const canRun = useCanRun()
  const [scenario, setScenario] = useState('')
  const [target, setTarget] = useState('')
  const [n, setN] = useState(500)
  const [concurrency, setConcurrency] = useState(16)
  const [running, setRunning] = useState(false)
  const [res, setRes] = useState<LoadResult>()

  const sc = scenario || scenarios[0]?.id
  const tg = target || targets[0]

  async function run() {
    if (!sc || !tg) return
    setRunning(true)
    try {
      setRes(await api.load(sc, tg, n, concurrency))
    } catch (e) {
      toast.error('Load run failed', { description: String((e as Error).message) })
    } finally {
      setRunning(false)
    }
  }

  const inputCls = 'rounded-lg border border-line bg-surface/60 px-2.5 py-1.5 text-sm text-text outline-none'
  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium text-text"><Gauge className="h-4 w-4 text-accent2" /> Load test</div>
      <div className="flex flex-wrap items-center gap-2">
        <select value={sc} onChange={(e) => setScenario(e.target.value)} className={inputCls}>
          {scenarios.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <select value={tg} onChange={(e) => setTarget(e.target.value)} className={inputCls}>
          {targets.map((t) => <option key={t} value={t}>{t}</option>)}
        </select>
        <label className="text-xs text-faint">N <input type="number" value={n} onChange={(e) => setN(+e.target.value)} className={`${inputCls} w-24`} /></label>
        <label className="text-xs text-faint">conc <input type="number" value={concurrency} onChange={(e) => setConcurrency(+e.target.value)} className={`${inputCls} w-20`} /></label>
        {canRun ? (
        <button onClick={run} disabled={running} className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50">
            {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Run
          </button>
        ) : <ReadOnly what="run load tests" />}
      </div>
      {res && (
        <div className="mt-4 grid grid-cols-4 gap-3">
          <Stat label="Throughput" value={res.throughput.toFixed(0)} unit="req/s" />
          <Stat label="p50 / p99" value={`${fmtMs(res.p50)} / ${fmtMs(res.p99)}`} unit="ms" />
          <Stat label="max" value={fmtMs(res.max)} unit="ms" />
          <Stat label="errors" value={String(res.errors)} tone={res.errors > 0 ? 'fail' : 'neutral'} />
        </div>
      )}
    </Card>
  )
}
