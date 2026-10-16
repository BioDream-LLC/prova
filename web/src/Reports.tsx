import { useMemo, useState } from 'react'
import { Download } from 'lucide-react'
import type * as echarts from 'echarts'
import { Page, Card, Stat, Outcome, Pill, fmtMs } from './ui'
import { EChart } from './charts'
import { api, useAsync, type KeyTally, type Report } from './api'

const periods = [7, 30, 90, 365]

const ms = fmtMs

const pctOf = (t: { pass: number; fail: number; indeterminate: number }) => {
  const n = t.pass + t.fail + t.indeterminate
  return n === 0 ? '—' : ((100 * t.pass) / n).toFixed(1)
}

function byDayOption(r: Report): echarts.EChartsOption {
  const axis = { color: '#93a0b8', fontSize: 11 }
  return {
    grid: { left: 36, right: 12, top: 28, bottom: 28 },
    legend: { top: 0, right: 0, textStyle: { color: '#93a0b8' }, itemWidth: 10, itemHeight: 10 },
    tooltip: { trigger: 'axis', backgroundColor: '#0f172a', borderColor: '#1f2a44', textStyle: { color: '#e5ebf5' } },
    xAxis: {
      type: 'category',
      data: r.byDay.map((d) => d.day.slice(5)),
      axisLabel: axis,
      axisLine: { lineStyle: { color: '#1f2a44' } },
    },
    yAxis: { type: 'value', minInterval: 1, axisLabel: axis, splitLine: { lineStyle: { color: 'rgba(31,42,68,0.6)' } } },
    series: [
      { name: 'Pass', type: 'bar', stack: 'v', data: r.byDay.map((d) => d.pass), itemStyle: { color: '#10b981' }, barMaxWidth: 18 },
      { name: 'Fail', type: 'bar', stack: 'v', data: r.byDay.map((d) => d.fail), itemStyle: { color: '#f43f5e' } },
      { name: 'Indeterminate', type: 'bar', stack: 'v', data: r.byDay.map((d) => d.indeterminate), itemStyle: { color: '#f59e0b', borderRadius: [3, 3, 0, 0] } },
    ],
  }
}

/** Spread is a single stacked bar of pass / fail / indeterminate. */
function Spread({ t }: { t: KeyTally }) {
  const n = t.pass + t.fail + t.indeterminate || 1
  return (
    <div className="flex h-1.5 w-28 overflow-hidden rounded-full bg-surface2" aria-hidden="true">
      <span className="bg-pass" style={{ width: `${(100 * t.pass) / n}%` }} />
      <span className="bg-fail" style={{ width: `${(100 * t.fail) / n}%` }} />
      <span className="bg-indet" style={{ width: `${(100 * t.indeterminate) / n}%` }} />
    </div>
  )
}

function Breakdown({ title, rows, latency = true }: { title: string; rows: KeyTally[]; latency?: boolean }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-line px-5 py-3 text-sm font-medium text-text">{title}</div>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-[11px] uppercase tracking-wider text-faint">
            <th className="px-5 py-2 font-medium" />
            <th className="px-3 py-2 text-right font-medium">Runs</th>
            <th className="px-3 py-2 text-right font-medium">Pass %</th>
            <th className="px-3 py-2 font-medium">Spread</th>
            {latency && <th className="px-5 py-2 text-right font-medium">p50 / p95 ms</th>}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr><td colSpan={5} className="px-5 py-4 text-muted">No runs in this period.</td></tr>
          )}
          {rows.map((r) => (
            <tr key={r.key} className="border-t border-line/60">
              <td className="max-w-[16rem] truncate px-5 py-2 text-text" title={r.label ?? r.key}>
                {r.label ?? <span className="font-mono text-xs">{r.key}</span>}
              </td>
              <td className="px-3 py-2 text-right tnum text-muted">{r.pass + r.fail + r.indeterminate}</td>
              <td className="px-3 py-2 text-right tnum text-text">{pctOf(r)}</td>
              <td className="px-3 py-2"><Spread t={r} /></td>
              {latency && <td className="px-5 py-2 text-right tnum text-muted">{ms(r.p50Ms)} / {ms(r.p95Ms)}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  )
}

/** Reports is the history of verdicts from the database. */
export function Reports() {
  const [days, setDays] = useState(30)
  const { data: r, error, loading } = useAsync(() => api.report(days), [days])
  const option = useMemo(() => (r ? byDayOption(r) : undefined), [r])

  return (
    <Page
      title="Reports"
      subtitle="Every verdict prova has reached, from its database"
      actions={
        <>
          <div className="flex rounded-lg border border-line bg-surface/60 p-0.5" role="group" aria-label="Period">
            {periods.map((d) => (
              <button
                key={d}
                onClick={() => setDays(d)}
                aria-pressed={d === days}
                className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${d === days ? 'bg-accent/20 text-text' : 'text-muted hover:text-text'}`}
              >
                {d === 365 ? '1 year' : `${d} days`}
              </button>
            ))}
          </div>
          <a
            href={`/api/reports/runs.csv?days=${days}`}
            className="flex items-center gap-1.5 rounded-lg border border-line bg-surface2 px-3 py-1.5 text-sm text-text hover:border-accent/50"
          >
            <Download className="h-3.5 w-3.5" aria-hidden="true" /> Download CSV
          </a>
        </>
      }
    >
      {error && <Card className="p-5 text-fail">{error}</Card>}
      {loading && !r && <div className="text-sm text-muted">Reading the database…</div>}
      {r && (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-5 gap-4">
            <Stat label="Verdicts" value={r.runs.toLocaleString()} delta={`${r.days} days to ${r.to.slice(0, 10)}`} />
            <Stat label="Pass rate" value={pctOf(r.totals)} unit="%" tone="pass" />
            <Stat label="Failed" value={r.totals.fail.toLocaleString()} tone="fail" />
            <Stat label="Indeterminate" value={r.totals.indeterminate.toLocaleString()} tone="indet" />
            <Stat
              label="Engines disagreed"
              value={r.engineDisagreements.toLocaleString()}
              tone={r.engineDisagreements ? 'fail' : 'neutral'}
              delta="casebook vs. quorum"
            />
          </div>

          <Card className="p-5">
            <div className="mb-2 text-sm font-medium text-text">Verdicts by day</div>
            {option && <EChart option={option} height={240} />}
          </Card>

          <div className="grid grid-cols-2 gap-4">
            <Breakdown title="By target" rows={r.byTarget} />
            <Breakdown title="By protocol" rows={r.byProtocol} />
            <Breakdown title="By scenario" rows={r.byScenario} />
            <Breakdown title="By person" rows={r.byActor} latency={false} />
          </div>

          <Card className="overflow-hidden">
            <div className="flex items-center justify-between border-b border-line px-5 py-3">
              <span className="text-sm font-medium text-text">Suite executions</span>
              <Pill>{r.suites.length}</Pill>
            </div>
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-[11px] uppercase tracking-wider text-faint">
                  <th className="px-5 py-2 font-medium">When</th>
                  <th className="px-3 py-2 font-medium">Suite</th>
                  <th className="px-3 py-2 font-medium">Target</th>
                  <th className="px-3 py-2 font-medium">Verdict</th>
                  <th className="px-3 py-2 text-right font-medium">Pass / fail / indet.</th>
                  <th className="px-3 py-2 text-right font-medium">Duration</th>
                  <th className="px-5 py-2 font-medium">By</th>
                </tr>
              </thead>
              <tbody>
                {r.suites.length === 0 && (
                  <tr><td colSpan={7} className="px-5 py-4 text-muted">No suite has been run in this period.</td></tr>
                )}
                {r.suites.map((s) => (
                  <tr key={s.id} className="border-t border-line/60">
                    <td className="px-5 py-2 tnum text-muted">{new Date(s.at).toLocaleString()}</td>
                    <td className="px-3 py-2 text-text">{s.name}</td>
                    <td className="px-3 py-2 font-mono text-xs text-muted">{s.target}</td>
                    <td className="px-3 py-2"><Outcome value={s.outcome} /></td>
                    <td className="px-3 py-2 text-right tnum text-muted">{s.pass} / {s.fail} / {s.indeterminate}</td>
                    <td className="px-3 py-2 text-right tnum text-muted">{s.durationMs} ms</td>
                    <td className="px-5 py-2 font-mono text-xs text-muted">{s.actor || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>

          <Card className="overflow-hidden">
            <div className="flex items-center justify-between border-b border-line px-5 py-3">
              <span className="text-sm font-medium text-text">Load tests</span>
              <Pill>{r.loads.length}</Pill>
            </div>
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-[11px] uppercase tracking-wider text-faint">
                  <th className="px-5 py-2 font-medium">When</th>
                  <th className="px-3 py-2 font-medium">Scenario</th>
                  <th className="px-3 py-2 font-medium">Target</th>
                  <th className="px-3 py-2 text-right font-medium">Requests × conc.</th>
                  <th className="px-3 py-2 text-right font-medium">Throughput</th>
                  <th className="px-3 py-2 text-right font-medium">p50 / p95 / p99 ms</th>
                  <th className="px-5 py-2 text-right font-medium">Errors</th>
                </tr>
              </thead>
              <tbody>
                {r.loads.length === 0 && (
                  <tr><td colSpan={7} className="px-5 py-4 text-muted">No load test has been run in this period.</td></tr>
                )}
                {r.loads.map((l, i) => (
                  <tr key={i} className="border-t border-line/60">
                    <td className="px-5 py-2 tnum text-muted">{new Date(l.at).toLocaleString()}</td>
                    <td className="px-3 py-2 text-text">{l.scenario}</td>
                    <td className="px-3 py-2 font-mono text-xs text-muted">{l.target}</td>
                    <td className="px-3 py-2 text-right tnum text-muted">{l.n} × {l.concurrency}</td>
                    <td className="px-3 py-2 text-right tnum text-text">{l.throughput.toFixed(0)}/s</td>
                    <td className="px-3 py-2 text-right tnum text-muted">{ms(l.p50)} / {ms(l.p95)} / {ms(l.p99)}</td>
                    <td className={`px-5 py-2 text-right tnum ${l.errors ? 'text-fail' : 'text-muted'}`}>{l.errors}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>

        </div>
      )}
    </Page>
  )
}
