import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import { useNavigate } from 'react-router-dom'
import { Page, Card, Stat, Outcome, Pill, fmtMs } from './ui'
import { EChart } from './charts'
import { api, useAsync, type Run } from './api'

function timeAgo(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function percentile(xs: number[], p: number): number {
  if (xs.length === 0) return 0
  const s = [...xs].sort((a, b) => a - b)
  return s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))]
}

export function Dashboard() {
  const navigate = useNavigate()
  const { data: runs = [], error } = useAsync(() => api.runs(), [])

  const stats = useMemo(() => {
    const total = runs.length
    const pass = runs.filter((r) => r.outcome === 'pass').length
    const indet = runs.filter((r) => r.outcome === 'indeterminate').length
    const fail = runs.filter((r) => r.outcome === 'fail').length
    const decided = pass + fail
    const rate = decided ? (pass / decided) * 100 : 0
    const p99 = percentile(runs.map((r) => r.latencyMs), 99)
    return { total, pass, fail, indet, rate, p99 }
  }, [runs])

  const latency = useMemo<EChartsOption>(() => {
    const chrono = [...runs].reverse()
    return {
      grid: { left: 44, right: 16, top: 24, bottom: 28 },
      tooltip: { trigger: 'axis' },
      xAxis: { type: 'category', data: chrono.map((r) => r.id), axisLine: { lineStyle: { color: '#1f2a44' } }, axisLabel: { color: '#5b6880' } },
      yAxis: { type: 'value', name: 'ms', nameTextStyle: { color: '#5b6880' }, splitLine: { lineStyle: { color: 'rgba(31,42,68,0.5)' } }, axisLabel: { color: '#5b6880' } },
      series: [{
        name: 'round-trip', type: 'line', smooth: true, symbol: 'circle', symbolSize: 6,
        data: chrono.map((r) => r.latencyMs),
        lineStyle: { color: '#6366f1', width: 2 }, itemStyle: { color: '#6366f1' },
        areaStyle: { color: 'rgba(99,102,241,0.10)' },
      }],
    }
  }, [runs])

  const distribution = useMemo<EChartsOption>(() => ({
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, textStyle: { color: '#93a0b8' }, icon: 'circle' },
    series: [{
      type: 'pie', radius: ['52%', '74%'], center: ['50%', '44%'], avoidLabelOverlap: true,
      itemStyle: { borderColor: '#0b1120', borderWidth: 3 },
      label: { show: false }, labelLine: { show: false },
      data: [
        { value: stats.pass, name: 'Pass', itemStyle: { color: '#10b981' } },
        { value: stats.fail, name: 'Fail', itemStyle: { color: '#f43f5e' } },
        { value: stats.indet, name: 'Indeterminate', itemStyle: { color: '#f59e0b' } },
      ],
    }],
  }), [stats])

  return (
    <Page
      title="Dashboard"
      subtitle="Live view of scenarios, verdicts and round-trip latency"
      actions={<Pill>{runs.length} runs</Pill>}
    >
      {error && <Card className="mb-4 border-fail/40 p-4 text-sm text-fail">Could not load runs: {error}</Card>}

      <div className="grid grid-cols-4 gap-4">
        <Stat label="Scenarios run" value={String(stats.total)} delta="most recent runs" />
        <Stat label="Pass rate" value={stats.rate.toFixed(1)} unit="%" tone="pass" delta={`${stats.pass} passed / ${stats.fail} failed`} />
        <Stat label="Indeterminate" value={String(stats.indet)} unit="open" tone="indet" delta="awaiting evidence" />
        <Stat label="p99 latency" value={fmtMs(stats.p99)} unit="ms" delta="round-trip" />
      </div>

      <div className="mt-4 grid grid-cols-3 gap-4">
        <Card className="col-span-2 p-5">
          <div className="mb-1 flex items-center justify-between">
            <h2 className="text-sm font-medium text-text">Round-trip latency</h2>
            <span className="text-xs text-faint">per run</span>
          </div>
          <EChart option={latency} height={280} />
        </Card>
        <Card className="p-5">
          <div className="mb-1 flex items-center justify-between">
            <h2 className="text-sm font-medium text-text">Verdicts</h2>
            <span className="text-xs text-faint">distribution</span>
          </div>
          <EChart option={distribution} height={280} />
        </Card>
      </div>

      <Card className="mt-4">
        <div className="flex items-center justify-between border-b border-line px-5 py-3">
          <h2 className="text-sm font-medium text-text">Recent runs</h2>
          <span className="text-xs text-faint">{runs.length} shown</span>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-faint">
              <th className="px-5 py-2.5 font-medium">Run</th>
              <th className="px-5 py-2.5 font-medium">Scenario</th>
              <th className="px-5 py-2.5 font-medium">Target</th>
              <th className="px-5 py-2.5 font-medium">Verdict</th>
              <th className="px-5 py-2.5 font-medium text-right">When</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r: Run) => (
              <tr
                key={r.id}
                onClick={() => navigate(`/proof?run=${r.id}`)}
                className="cursor-pointer border-t border-line/60 hover:bg-surface2/40"
              >
                <td className="px-5 py-3 font-mono text-xs text-muted">{r.id}</td>
                <td className="px-5 py-3 text-text">{r.scenario}</td>
                <td className="px-5 py-3"><Pill>{r.target}</Pill></td>
                <td className="px-5 py-3"><Outcome value={r.outcome} /></td>
                <td className="px-5 py-3 text-right text-xs text-muted tnum">{timeAgo(r.at)}</td>
              </tr>
            ))}
            {runs.length === 0 && (
              <tr><td colSpan={5} className="px-5 py-8 text-center text-sm text-faint">No runs yet — start one from the Scenarios screen.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </Page>
  )
}
