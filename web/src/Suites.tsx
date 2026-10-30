import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Loader2, Gauge, Repeat, FileUp } from 'lucide-react'
import { toast } from 'sonner'
import { Page, Card, Pill, Outcome, Stat, fmtMs } from './ui'
import { api, useAsync, type SuiteResult, type LoadResult, type ReplayResult } from './api'
import { EChart } from './charts'
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
    <Page title="Suites" subtitle="Regression and certification packs, load tests and traffic replay — proved, not just passed">
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

          <LoadPanel scenarios={scenarios.map((s) => ({ id: s.id, name: s.name, protocol: s.protocol, target: s.target }))} targets={targets.map((t) => ({ name: t.name, protocol: t.protocol }))} />
          <ReplayPanel targets={targets.map((t) => ({ name: t.name, protocol: t.protocol }))} />
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

type Named = { name: string; protocol: string }

const inputCls = 'rounded-lg border border-line bg-surface/60 px-2.5 py-1.5 text-sm text-text outline-none'

/** LoadStats shows a load run's numbers and its per-second timeline. */
function LoadStats({ res }: { res: LoadResult }) {
  const outcomes = Object.entries(res.outcomes).map(([k, v]) => `${v} ${k}`).join(' · ')
  const option = useMemo(() => {
    const t = res.timeline ?? []
    return {
      grid: { left: 44, right: 44, top: 24, bottom: 24 },
      tooltip: { trigger: 'axis' as const },
      legend: { data: ['requests', 'p95 ms'], textStyle: { color: '#93a0b8' }, top: 0 },
      xAxis: { type: 'category' as const, data: t.map((x) => `${x.at}s`), axisLabel: { color: '#93a0b8' } },
      yAxis: [
        { type: 'value' as const, axisLabel: { color: '#93a0b8' }, splitLine: { lineStyle: { color: 'rgba(31,42,68,0.6)' } } },
        { type: 'value' as const, axisLabel: { color: '#93a0b8' }, splitLine: { show: false } },
      ],
      series: [
        { name: 'requests', type: 'bar' as const, data: t.map((x) => x.sent), itemStyle: { color: '#6366f1' } },
        { name: 'p95 ms', type: 'line' as const, yAxisIndex: 1, data: t.map((x) => +x.p95.toFixed(3)), itemStyle: { color: '#f59e0b' } },
      ],
    }
  }, [res])
  return (
    <div className="mt-4">
      <div className="grid grid-cols-4 gap-3">
        <Stat label="Throughput" value={res.throughput.toFixed(0)} unit="req/s" delta={`${res.n} requests · ${res.concurrency} connections`} />
        <Stat label="p50 / p99" value={`${fmtMs(res.p50)} / ${fmtMs(res.p99)}`} unit="ms" delta={`mean ${fmtMs(res.mean)} ms`} />
        <Stat label="max" value={fmtMs(res.max)} unit="ms" delta={`${(res.durationMs / 1000).toFixed(1)} s`} />
        <Stat label="errors" value={String(res.errors)} tone={res.errors > 0 ? 'fail' : 'neutral'} delta={outcomes} />
      </div>
      {res.firstError && <div className="mt-2 font-mono text-xs text-fail">first error: {res.firstError}</div>}
      {(res.timeline?.length ?? 0) > 1 && <div className="mt-3"><EChart option={option} height={200} /></div>}
    </div>
  )
}

function LoadPanel({ scenarios, targets }: { scenarios: (Named & { id: string; target?: string })[]; targets: Named[] }) {
  const canRun = useCanRun()
  const [scenario, setScenario] = useState('')
  const [target, setTarget] = useState('')
  const [n, setN] = useState(500)
  const [seconds, setSeconds] = useState(0)
  const [rate, setRate] = useState(0)
  const [concurrency, setConcurrency] = useState(16)
  const [running, setRunning] = useState(false)
  const [res, setRes] = useState<LoadResult>()

  const sc = scenario || scenarios[0]?.id
  const chosen = scenarios.find((s) => s.id === sc)
  // Only the targets that speak the chosen scenario's protocol can run it.
  const usable = targets.filter((t) => t.protocol === chosen?.protocol).map((t) => t.name)
  const tg = usable.includes(target) ? target : chosen?.target && usable.includes(chosen.target) ? chosen.target : usable[0]

  async function run() {
    if (!sc || !tg) return
    setRunning(true)
    try {
      setRes(await api.load(sc, tg, n, concurrency, rate, seconds))
    } catch (e) {
      toast.error('Load run failed', { description: String((e as Error).message) })
    } finally {
      setRunning(false)
    }
  }

  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium text-text"><Gauge className="h-4 w-4 text-accent2" /> Load test</div>
      <div className="flex flex-wrap items-center gap-2">
        <select value={sc} onChange={(e) => setScenario(e.target.value)} className={`${inputCls} max-w-[22rem]`} aria-label="Scenario">
          {scenarios.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <select value={tg} onChange={(e) => setTarget(e.target.value)} className={inputCls} aria-label="Target">
          {usable.map((t) => <option key={t} value={t}>{t}</option>)}
        </select>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <label className="text-xs text-faint">requests <input aria-label="Requests" type="number" min={1} value={n} onChange={(e) => setN(+e.target.value)} className={`${inputCls} w-24`} disabled={seconds > 0} /></label>
        <label className="text-xs text-faint">or seconds <input aria-label="Seconds" type="number" min={0} max={600} value={seconds} onChange={(e) => setSeconds(+e.target.value)} className={`${inputCls} w-20`} /></label>
        <label className="text-xs text-faint">rate/s <input aria-label="Rate per second" type="number" min={0} value={rate} onChange={(e) => setRate(+e.target.value)} className={`${inputCls} w-24`} title="0 sends as fast as the target answers" /></label>
        <label className="text-xs text-faint">connections <input aria-label="Connections" type="number" min={1} max={512} value={concurrency} onChange={(e) => setConcurrency(+e.target.value)} className={`${inputCls} w-20`} /></label>
        {canRun ? (
          <button onClick={run} disabled={running} className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50">
            {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Run
          </button>
        ) : <ReadOnly what="run load tests" />}
      </div>
      {res && <LoadStats res={res} />}
    </Card>
  )
}

/** ReplayPanel replays recorded or captured traffic live against a target. */
function ReplayPanel({ targets }: { targets: Named[] }) {
  const canRun = useCanRun()
  const [file, setFile] = useState<{ name: string; text: string }>()
  const [kind, setKind] = useState<'cassette' | 'capture'>('cassette')
  const [protocol, setProtocol] = useState('fix')
  const [dialect, setDialect] = useState('generic')
  const [mask, setMask] = useState(true)
  const [target, setTarget] = useState('')
  const [speed, setSpeed] = useState(0)
  const [rate, setRate] = useState(0)
  const [n, setN] = useState(0)
  const [concurrency, setConcurrency] = useState(8)
  const [running, setRunning] = useState(false)
  const [res, setRes] = useState<ReplayResult>()

  // A cassette names its protocol on every line; a capture's is chosen here.
  const cassetteProtocol = useMemo(() => {
    if (kind !== 'cassette' || !file) return ''
    try { return (JSON.parse(file.text.split('\n')[0]) as { protocol?: string }).protocol ?? '' } catch { return '' }
  }, [file, kind])
  const proto = kind === 'capture' ? protocol : cassetteProtocol
  const usable = targets.filter((t) => !proto || t.protocol === proto).map((t) => t.name)
  const tg = usable.includes(target) ? target : usable[0]

  async function run() {
    if (!file || !tg) return
    setRunning(true)
    try {
      const body = kind === 'cassette' ? { cassette: file.text } : { capture: file.text, protocol, dialect, mask }
      setRes(await api.replay({ ...body, target: tg, n, concurrency, rate, speed }))
    } catch (e) {
      toast.error('Replay failed', { description: String((e as Error).message) })
    } finally {
      setRunning(false)
    }
  }

  return (
    <Card className="p-4">
      <div className="mb-1 flex items-center gap-2 text-sm font-medium text-text"><Repeat className="h-4 w-4 text-accent2" /> Replay traffic</div>
      <p className="mb-3 text-xs text-muted">Send recorded or captured traffic to a target, live, and judge every reply. A cassette comes from <span className="font-mono">prova run --record</span> or <span className="font-mono">prova import</span>; a capture is a FIX log, an ISO 8583 dump (one message per line, hex) or ISO 20022 files.</p>
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label="What the file is" value={kind} onChange={(e) => setKind(e.target.value as 'cassette' | 'capture')} className={inputCls}>
          <option value="cassette">cassette (.jsonl)</option>
          <option value="capture">raw capture</option>
        </select>
        <label className="flex cursor-pointer items-center gap-1.5 rounded-lg border border-line bg-surface2 px-3 py-1.5 text-sm text-muted hover:text-text">
          <FileUp className="h-3.5 w-3.5" /> {file ? file.name : 'choose a file'}
          <input aria-label="Traffic file" type="file" className="hidden" onChange={async (e) => {
            const f = e.target.files?.[0]
            if (f) setFile({ name: f.name, text: await f.text() })
            e.target.value = ''
          }} />
        </label>
        {kind === 'capture' && (
          <>
            <select aria-label="Capture protocol" value={protocol} onChange={(e) => setProtocol(e.target.value)} className={inputCls}>
              <option value="fix">FIX</option><option value="iso8583">ISO 8583</option><option value="iso20022">ISO 20022</option>
            </select>
            {protocol === 'iso8583' && (
              <select aria-label="Capture dialect" value={dialect} onChange={(e) => setDialect(e.target.value)} className={inputCls}>
                <option value="generic">generic</option><option value="visa">Visa-style</option><option value="mastercard">Mastercard-style</option>
              </select>
            )}
            <label className="flex items-center gap-1.5 text-xs text-muted" title="Replace card numbers, IBANs, account ids and names with stable stand-ins">
              <input type="checkbox" checked={mask} onChange={(e) => setMask(e.target.checked)} /> mask cardholder data
            </label>
          </>
        )}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <select aria-label="Replay target" value={tg} onChange={(e) => setTarget(e.target.value)} className={inputCls}>
          {usable.map((t) => <option key={t} value={t}>{t}</option>)}
        </select>
        <label className="text-xs text-faint" title="1 keeps the recorded timing; 2 is twice as fast; 0 ignores it">speed <input aria-label="Speed" type="number" min={0} step={0.5} value={speed} onChange={(e) => setSpeed(+e.target.value)} className={`${inputCls} w-16`} /></label>
        <label className="text-xs text-faint">rate/s <input aria-label="Replay rate" type="number" min={0} value={rate} onChange={(e) => setRate(+e.target.value)} className={`${inputCls} w-20`} /></label>
        <label className="text-xs text-faint" title="0 sends every recording once; more loops over them">requests <input aria-label="Replay requests" type="number" min={0} value={n} onChange={(e) => setN(+e.target.value)} className={`${inputCls} w-20`} /></label>
        <label className="text-xs text-faint">connections <input aria-label="Replay connections" type="number" min={1} max={512} value={concurrency} onChange={(e) => setConcurrency(+e.target.value)} className={`${inputCls} w-16`} /></label>
        {canRun ? (
          <button onClick={run} disabled={running || !file} className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50">
            {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />} Replay
          </button>
        ) : <ReadOnly what="replay traffic" />}
      </div>
      {res?.import && (
        <div className="mt-3 text-xs text-muted">
          imported {res.import.kept} requests of {res.import.read} messages ({res.import.skipped} skipped){res.import.masked ? ', masked' : ''}
          {res.import.notes?.map((x, i) => <div key={i} className="text-faint">{x}</div>)}
        </div>
      )}
      {res && <LoadStats res={res.load} />}
    </Card>
  )
}
