import { useMemo } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useSearchParams } from 'react-router-dom'
import { ShieldCheck, Check, X } from 'lucide-react'
import { Page, Outcome, Pill, Card } from './ui'
import { api, useAsync, type Run, type Label } from './api'

const NODE_W = 236
const NODE_H = 76

type Status = 'in' | 'out' | 'evidence' | 'undec'

type ClaimData = { label: string; detail: string; status: Status }

/** Longest-path layered layout, left-to-right. Dependency-free; tolerates attack cycles. */
function layout(nodes: Node[], edges: Edge[]): Node[] {
  const layer = new Map<string, number>(nodes.map((n) => [n.id, 0]))
  for (let i = 0; i < nodes.length; i++) {
    let changed = false
    for (const e of edges) {
      const next = (layer.get(e.source) ?? 0) + 1
      if (next > (layer.get(e.target) ?? 0)) { layer.set(e.target, next); changed = true }
    }
    if (!changed) break
  }
  const byLayer = new Map<number, string[]>()
  for (const n of nodes) {
    const l = layer.get(n.id) ?? 0
    if (!byLayer.has(l)) byLayer.set(l, [])
    byLayer.get(l)!.push(n.id)
  }
  const colGap = NODE_W + 92
  const rowGap = NODE_H + 28
  const pos = new Map<string, { x: number; y: number }>()
  for (const [l, ids] of byLayer) {
    const offset = ((ids.length - 1) * rowGap) / 2
    ids.forEach((id, i) => pos.set(id, { x: l * colGap, y: i * rowGap - offset }))
  }
  return nodes.map((n) => ({ ...n, position: pos.get(n.id) ?? n.position }))
}

function ClaimNode({ data }: NodeProps) {
  const d = data as ClaimData
  const ring = {
    in: 'ring-pass/50 bg-pass/5',
    out: 'ring-fail/40 bg-fail/5 opacity-75',
    evidence: 'ring-accent2/40 bg-accent2/5',
    undec: 'ring-indet/45 bg-indet/5',
  }[d.status]
  const dot = { in: 'bg-pass', out: 'bg-fail', evidence: 'bg-accent2', undec: 'bg-indet' }[d.status]
  const tag = { in: 'accepted', out: 'defeated', evidence: 'evidence', undec: 'undecided' }[d.status]
  return (
    <div className={`w-[236px] rounded-xl border border-line px-3.5 py-3 ring-1 backdrop-blur-sm ${ring}`}>
      <Handle type="target" position={Position.Left} className="!h-2 !w-2 !border-0 !bg-faint" />
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 text-sm font-medium leading-tight text-text">
          <span className={`h-2 w-2 shrink-0 rounded-full ${dot}`} />
          {d.label}
        </span>
        <span className="shrink-0 text-[10px] uppercase tracking-wider text-faint">{tag}</span>
      </div>
      {d.detail && <div className="mt-1 pl-4 font-mono text-[11px] text-muted">{d.detail}</div>}
      <Handle type="source" position={Position.Right} className="!h-2 !w-2 !border-0 !bg-faint" />
    </div>
  )
}

const nodeTypes = { claim: ClaimNode }

/** Fact renders a single certificate row: either a boolean check or a labelled value. */
function Fact({ label, ok, value }: { label: string; ok?: boolean; value?: string }) {
  return (
    <div className="flex items-center gap-1.5">
      {value !== undefined ? (
        <span className="font-mono text-text">{value}</span>
      ) : ok ? (
        <Check className="h-3.5 w-3.5 text-pass" />
      ) : (
        <X className="h-3.5 w-3.5 text-faint" />
      )}
      <span className="text-muted">{label}</span>
    </div>
  )
}

const edgeColor = (k: string) => (k === 'rebut' ? '#f43f5e' : k === 'undermine' ? '#f59e0b' : '#22d3ee')

export function Proof() {
  const [params] = useSearchParams()
  const runParam = params.get('run') ?? undefined
  const { data: run } = useAsync<Run | undefined>(
    () => (runParam ? api.run(runParam) : api.runs().then((r) => r[0])),
    [runParam],
  )

  const { nodes, edges } = useMemo(() => {
    if (!run) return { nodes: [] as Node[], edges: [] as Edge[] }
    const { labels } = run.result
    const framework = { args: run.result.framework?.args ?? [], attacks: run.result.framework?.attacks ?? [] }
    const incoming = new Set(framework.attacks.map((a) => a.to))
    const status = (id: string): Status => {
      const l: Label = labels[id] ?? 'undec'
      if (l === 'in') return incoming.has(id) ? 'in' : 'evidence'
      if (l === 'out') return 'out'
      return 'undec'
    }
    const rawNodes: Node[] = framework.args.map((a) => ({
      id: a.id,
      type: 'claim',
      position: { x: 0, y: 0 },
      data: { label: a.claim, detail: (a.cites ?? []).join(' · '), status: status(a.id) } satisfies ClaimData,
    }))
    const rawEdges: Edge[] = framework.attacks.map((a, i) => ({
      id: `e${i}`,
      source: a.from,
      target: a.to,
      label: a.kind,
      animated: a.kind === 'rebut',
      style: { stroke: edgeColor(a.kind), strokeWidth: 1.8 },
      labelStyle: { fill: edgeColor(a.kind), fontSize: 10, textTransform: 'uppercase' },
      labelBgStyle: { fill: '#0b1120' },
      markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor(a.kind), width: 16, height: 16 },
    }))
    return { nodes: layout(rawNodes, rawEdges), edges: rawEdges }
  }, [run])

  const verdictLine = (() => {
    if (!run) return ''
    switch (run.result.outcome) {
      case 'pass': return 'The pass claim holds under grounded semantics — every argument against it is itself defeated.'
      case 'fail': return 'A violation stands: the pass claim is defeated, so the host did not behave as the rule requires.'
      default: return 'The evidence does not settle the verdict. prova names below exactly what would decide it.'
    }
  })()

  return (
    <Page
      title="Proof"
      subtitle="Why this verdict holds — a formal argumentation proof, not an AI explanation"
      actions={run ? <><Outcome value={run.outcome} /><Pill>{run.id}</Pill></> : undefined}
    >
      {!run && <Card className="p-6 text-sm text-faint">No run selected. Start one from Scenarios.</Card>}

      {run && (
        <div className="grid h-full grid-cols-[1fr_340px] gap-4">
          <div className="overflow-hidden rounded-[var(--radius)] border border-line bg-surface/40">
            <ReactFlow
              key={run.id}
              nodes={nodes}
              edges={edges}
              nodeTypes={nodeTypes}
              fitView
              fitViewOptions={{ padding: 0.2, maxZoom: 1.1, minZoom: 0.4 }}
              proOptions={{ hideAttribution: true }}
              minZoom={0.3}
            >
              <Background variant={BackgroundVariant.Dots} gap={22} size={1} color="#1f2a44" />
              <MiniMap pannable zoomable maskColor="rgba(11,17,32,0.7)" nodeColor="#1f2a44" style={{ background: '#0f172a', border: '1px solid #1f2a44' }} />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>

          <div className="flex flex-col gap-3 overflow-auto">
            <Card className="p-4">
              <div className="text-xs font-medium uppercase tracking-wider text-faint">Verdict</div>
              <div className="mt-2 flex items-center gap-2"><Outcome value={run.outcome} /></div>
              <p className="mt-3 text-sm text-muted">{verdictLine}</p>
              <p className="mt-2 text-xs text-faint">{run.scenario} → {run.target}</p>
            </Card>

            {run.result.certificate && (
              <Card className="p-4">
                <div className="flex items-center justify-between">
                  <div className="text-xs font-medium uppercase tracking-wider text-faint">Certificate</div>
                  <span className="flex items-center gap-1.5 text-xs text-pass">
                    <ShieldCheck className="h-3.5 w-3.5" />
                    re-checked
                  </span>
                </div>
                <div className="mt-2 flex items-center gap-2 text-sm">
                  <span className="text-muted">engine</span>
                  <span className="rounded border border-line bg-surface2 px-1.5 py-0.5 font-mono text-xs text-text">{run.result.certificate.engine}</span>
                </div>
                <dl className="mt-3 grid grid-cols-2 gap-y-1.5 text-xs">
                  <Fact label="grounded" value={run.result.certificate.queryLabel} />
                  <Fact label="grounded re-proved" ok={run.result.certificate.groundedVerified} />
                  <Fact label="complete ext." ok={run.result.certificate.complete} />
                  <Fact label="stable exists" ok={run.result.certificate.stableExists} />
                  <Fact label="skeptical" ok={run.result.certificate.skeptical} />
                  <Fact label="credulous" ok={run.result.certificate.credulous} />
                </dl>
              </Card>
            )}

            {run.result.crossCheck && (
              <Card className={`p-4 ${run.result.crossCheck.agrees ? '' : 'border-fail/50'}`}>
                <div className="flex items-center justify-between">
                  <div className="text-xs font-medium uppercase tracking-wider text-faint">Second engine</div>
                  {run.result.crossCheck.agrees ? (
                    <span className="flex items-center gap-1.5 text-xs text-pass">
                      <Check className="h-3.5 w-3.5" />
                      agrees
                    </span>
                  ) : (
                    <span className="flex items-center gap-1.5 text-xs text-fail">
                      <X className="h-3.5 w-3.5" />
                      disagrees
                    </span>
                  )}
                </div>
                <div className="mt-2 flex items-center gap-2 text-sm">
                  <span className="rounded border border-line bg-surface2 px-1.5 py-0.5 font-mono text-xs text-text">{run.result.crossCheck.engine}</span>
                  <Outcome value={run.result.crossCheck.outcome} />
                </div>
                <p className="mt-2 text-xs text-muted">
                  quorum's casebook <span className="font-mono text-text">{run.result.crossCheck.casebook}</span>, compiled and
                  exhaustively validated by cbc, decided the same case by a separate route.
                </p>
                <div className="mt-2 flex flex-wrap gap-1">
                  {(run.result.crossCheck.readings ?? []).map((r) => (
                    <span key={r} className="rounded border border-line bg-surface2 px-1.5 py-0.5 font-mono text-[10px] text-faint">{r}</span>
                  ))}
                </div>
                <div className="mt-2 font-mono text-[10px] text-faint">cbc fingerprint {run.result.crossCheck.fingerprint}</div>
              </Card>
            )}

            <Card className="p-4">
              <div className="mb-2 text-xs font-medium uppercase tracking-wider text-faint">Proof chain</div>
              <ol className="flex flex-col gap-2.5">
                {(run.result.chain ?? []).map((s, i) => (
                  <li key={i} className="relative pl-5 text-sm">
                    <span className={`absolute left-0 top-1 h-2 w-2 rounded-full ${s.status === 'in' ? 'bg-pass' : s.status === 'out' ? 'bg-fail' : 'bg-indet'}`} />
                    <span className="text-text">{s.claim}</span>
                    <span className="text-muted"> — {s.because}</span>
                    {s.cites && s.cites.length > 0 && (
                      <div className="mt-0.5 flex flex-wrap gap-1">
                        {s.cites.map((c) => (
                          <span key={c} className="rounded border border-line bg-surface2 px-1.5 py-0.5 font-mono text-[10px] text-faint">{c}</span>
                        ))}
                      </div>
                    )}
                  </li>
                ))}
              </ol>
            </Card>

            {run.result.settles && run.result.settles.length > 0 && (
              <Card className="border-indet/40 p-4">
                <div className="mb-1 text-xs font-medium uppercase tracking-wider text-indet">Would settle it</div>
                <ul className="flex flex-col gap-1">
                  {run.result.settles.map((s) => (
                    <li key={s} className="font-mono text-xs text-muted">{s}</li>
                  ))}
                </ul>
              </Card>
            )}

            <Card className="p-4 text-xs text-faint">
              <span className="text-accent2">●</span> undercut &nbsp;
              <span className="text-indet">●</span> undermine &nbsp;
              <span className="text-fail">●</span> rebut
            </Card>
          </div>
        </div>
      )}
    </Page>
  )
}
