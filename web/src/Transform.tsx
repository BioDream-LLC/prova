import { useEffect, useState } from 'react'
import { ArrowRightLeft, Info } from 'lucide-react'
import { Page, Card, Pill } from './ui'
import { api, type Pair, type TransformResult } from './api'

type Dir = 'mt2mx' | 'mx2mt'

export function Transform() {
  const [pairs, setPairs] = useState<Pair[]>([])
  const [samples, setSamples] = useState<Record<string, string>>({})
  const [pair, setPair] = useState('MT103')
  const [dir, setDir] = useState<Dir>('mt2mx')
  const [input, setInput] = useState('')
  const [res, setRes] = useState<TransformResult>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.transformPairs().then((p) => {
      setPairs(p.pairs)
      setSamples(p.samples)
      setInput(p.samples.MT103 ?? '')
    }).catch((e) => setError(String(e)))
  }, [])

  const choose = (mt: string) => {
    setPair(mt)
    setDir('mt2mx')
    setInput(samples[mt] ?? '')
    setRes(undefined)
    setError(undefined)
  }

  const convert = async () => {
    setBusy(true)
    setError(undefined)
    try {
      setRes(await api.transform(dir, input))
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
      setRes(undefined)
    } finally {
      setBusy(false)
    }
  }

  const swap = () => {
    setDir(dir === 'mt2mx' ? 'mx2mt' : 'mt2mx')
    setInput(res?.result ?? '')
    setRes(undefined)
    setError(undefined)
  }

  const current = pairs.find((p) => p.mt === pair)
  const fromLabel = res?.from ?? (dir === 'mt2mx' ? current?.mt ?? 'MT' : 'ISO 20022 ' + (current?.mx ?? 'MX'))
  const toLabel = res?.to ?? (dir === 'mt2mx' ? 'ISO 20022 ' + (current?.mx ?? 'MX') : current?.mt ?? 'MT')

  return (
    <Page
      title="Transform"
      subtitle="MT ↔ MX for the ISO 20022 migration: eight message pairs, each MX output valid against the official schema — then drive and prove the result"
      actions={<Pill>{dir === 'mt2mx' ? 'MT → MX' : 'MX → MT'}</Pill>}
    >
      <div className="flex flex-col gap-4">
        <Card className="flex flex-wrap items-center gap-2 p-3">
          {pairs.map((p) => (
            <button key={p.mt} onClick={() => choose(p.mt)} title={p.name} aria-pressed={pair === p.mt}
              className={`rounded-lg px-3 py-1.5 text-xs transition-colors ${pair === p.mt ? 'bg-accent/20 text-text ring-1 ring-accent/30' : 'border border-line bg-surface2 text-muted hover:text-text'}`}>
              <span className="font-medium">{p.mt}</span> <span className="text-faint">↔ {p.mx}</span>
            </button>
          ))}
        </Card>
        <Card className="flex items-center justify-between p-4">
          <div className="flex items-center gap-3 text-sm">
            <span className="font-medium text-text">{fromLabel}</span>
            <ArrowRightLeft className="h-4 w-4 text-accent" />
            <span className="font-medium text-text">{toLabel}</span>
            {current && <span className="text-xs text-faint">{current.name}</span>}
          </div>
          <div className="flex items-center gap-2">
            <button onClick={swap} disabled={!res} className="rounded-lg border border-line bg-surface2 px-3 py-1.5 text-sm text-muted hover:text-text disabled:opacity-40">
              Swap direction
            </button>
            <button onClick={convert} disabled={busy || !input} className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50">
              {busy ? 'Converting…' : 'Convert'}
            </button>
          </div>
        </Card>

        <div className="grid grid-cols-2 gap-4">
          <Card className="flex flex-col p-0">
            <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">{fromLabel} — input</div>
            <textarea value={input} onChange={(e) => setInput(e.target.value)} spellCheck={false} aria-label="Message to transform"
              className="min-h-[460px] flex-1 resize-none rounded-b-[var(--radius)] bg-transparent px-4 py-3 font-mono text-xs leading-relaxed text-text outline-none" />
          </Card>
          <Card className="flex flex-col p-0">
            <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">{toLabel} — output</div>
            {error ? (
              <div className="px-4 py-3 font-mono text-xs text-fail">{error}</div>
            ) : (
              <>
                {res?.notes && res.notes.length > 0 && (
                  <ul className="flex flex-col gap-1 border-b border-line px-4 py-2.5">
                    {res.notes.map((n, i) => (
                      <li key={i} className="flex gap-2 text-xs text-muted"><Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-accent2" />{n}</li>
                    ))}
                  </ul>
                )}
                <pre className="min-h-[400px] flex-1 overflow-auto whitespace-pre-wrap px-4 py-3 font-mono text-xs leading-relaxed text-muted">
                  {res?.result || 'Press Convert to transform the message.'}
                </pre>
              </>
            )}
          </Card>
        </div>
      </div>
    </Page>
  )
}
