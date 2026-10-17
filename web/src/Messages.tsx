import { useMemo, useState } from 'react'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { Search, ArrowUpRight, ArrowDownLeft } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { Page, Pill, Card, fmtMs } from './ui'
import { api, useAsync, type Run } from './api'

type FieldRow = { key: string; value: string }

const col = createColumnHelper<FieldRow>()
const columns = [
  col.accessor('key', { header: 'Field', cell: (c) => <span className="font-mono text-accent2">{c.getValue()}</span> }),
  col.accessor('value', { header: 'Value', cell: (c) => <span className="font-mono text-muted">{c.getValue()}</span> }),
]

export function Messages() {
  const [params] = useSearchParams()
  const runParam = params.get('run') ?? undefined
  const { data: run } = useAsync<Run | undefined>(
    () => (runParam ? api.run(runParam) : api.runs().then((r) => r[0])),
    [runParam],
  )

  const messages = run?.messages ?? []
  const [idx, setIdx] = useState(0)
  const active = messages[Math.min(idx, Math.max(0, messages.length - 1))]

  const [filter, setFilter] = useState('')
  const data = useMemo<FieldRow[]>(
    () => (active ? Object.entries(active.fields).map(([key, value]) => ({ key, value })) : []),
    [active],
  )

  const table = useReactTable({
    data,
    columns,
    state: { globalFilter: filter },
    onGlobalFilterChange: setFilter,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  return (
    <Page
      title="Messages"
      subtitle="Field-level inspector for the message exchange of a run"
      actions={run ? <Pill>{run.id} · {run.scenario}</Pill> : undefined}
    >
      {!run && <Card className="p-6 text-sm text-faint">No run selected. Start one from Scenarios.</Card>}

      {run && (
        <div className="grid h-full grid-cols-[1fr_1fr] gap-4">
          <div className="flex min-h-0 flex-col">
            <div className="mb-3 flex gap-2">
              {messages.map((m, i) => (
                <button
                  key={i}
                  onClick={() => setIdx(i)}
                  className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ${i === idx ? 'border-accent/40 bg-accent/10 text-text' : 'border-line bg-surface/40 text-muted hover:bg-surface2/50'}`}
                >
                  {m.dir === 'out' ? <ArrowUpRight className="h-3.5 w-3.5 text-accent2" /> : <ArrowDownLeft className="h-3.5 w-3.5 text-pass" />}
                  <span className="font-mono text-xs">{m.kind}</span>
                  <span className="text-xs text-faint">{m.dir === 'out' ? 'sent' : `recv ${fmtMs(m.latencyMs)} ms`}</span>
                </button>
              ))}
            </div>

            <div className="mb-3 flex items-center gap-2 rounded-lg border border-line bg-surface/60 px-3 py-2">
              <Search className="h-4 w-4 text-faint" />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter fields…"
                className="w-full bg-transparent text-sm text-text outline-none placeholder:text-faint"
              />
              <span className="text-xs text-faint tnum">{table.getFilteredRowModel().rows.length}/{data.length}</span>
            </div>

            <div className="min-h-0 flex-1 overflow-auto rounded-[var(--radius)] border border-line bg-surface/40">
              <table className="w-full text-sm">
                <thead>
                  {table.getHeaderGroups().map((hg) => (
                    <tr key={hg.id} className="border-b border-line text-left text-xs uppercase tracking-wider text-faint">
                      {hg.headers.map((h) => (
                        <th key={h.id} className="px-5 py-2.5 font-medium">{flexRender(h.column.columnDef.header, h.getContext())}</th>
                      ))}
                    </tr>
                  ))}
                </thead>
                <tbody>
                  {table.getRowModel().rows.map((row) => (
                    <tr key={row.id} className="border-t border-line/50 hover:bg-surface2/40">
                      {row.getVisibleCells().map((cell) => (
                        <td key={cell.id} className="px-5 py-2.5">{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="flex min-h-0 flex-col overflow-hidden rounded-[var(--radius)] border border-line bg-surface/40">
            <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
              <span className="font-mono text-xs text-muted">{active?.kind} · wire</span>
              <span className="text-xs text-faint">{active?.summary}</span>
            </div>
            <pre className="min-h-0 flex-1 overflow-auto p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all text-muted">{active?.raw?.split("\x01").join("|")}</pre>
          </div>
        </div>
      )}
    </Page>
  )
}
