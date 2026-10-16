import { useState } from 'react'
import { ArrowRightLeft } from 'lucide-react'
import { Page, Card, Pill } from './ui'
import { api } from './api'
import { useCanRun } from './session'
import { ReadOnly } from './ReadOnly'

const SAMPLE_MT = `{1:F01BANKDEFFAXXX0000000000}{2:I103BANKFRPPAXXXN}{4:
:20:E2E-MT-1
:23B:CRED
:32A:261015EUR90,00
:52A:BANKDEFFXXX
:50K:/ACC-EUR-1
JOHN DOE
:57A:BANKFRPPXXX
:59:/ACC-FR-9
JANE ROE
:71A:SHA
-}`

type Dir = 'mt2mx' | 'mx2mt'

export function Transform() {
  const canRun = useCanRun()
  const [dir, setDir] = useState<Dir>('mt2mx')
  const [input, setInput] = useState(SAMPLE_MT)
  const [output, setOutput] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  const convert = async () => {
    setBusy(true)
    setError(undefined)
    try {
      const res = await api.transform(dir, input)
      setOutput(res.result)
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
      setOutput('')
    } finally {
      setBusy(false)
    }
  }

  const swap = () => {
    const next: Dir = dir === 'mt2mx' ? 'mx2mt' : 'mt2mx'
    setDir(next)
    setInput(output || '')
    setOutput('')
    setError(undefined)
  }

  const fromLabel = dir === 'mt2mx' ? 'SWIFT MT103' : 'ISO 20022 pacs.008'
  const toLabel = dir === 'mt2mx' ? 'ISO 20022 pacs.008' : 'SWIFT MT103'

  return (
    <Page
      title="Transform"
      subtitle="MT ↔ MX message transformation for the ISO 20022 migration — then drive and prove the result"
      actions={<Pill>{dir === 'mt2mx' ? 'MT → MX' : 'MX → MT'}</Pill>}
    >
      <div className="flex flex-col gap-4">
        <Card className="flex items-center justify-between p-4">
          <div className="flex items-center gap-3 text-sm">
            <span className="font-medium text-text">{fromLabel}</span>
            <ArrowRightLeft className="h-4 w-4 text-accent" />
            <span className="font-medium text-text">{toLabel}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={swap}
              className="rounded-lg border border-line bg-surface2 px-3 py-1.5 text-sm text-muted hover:text-text"
            >
              Swap direction
            </button>
            {canRun ? (
            <button
                onClick={convert}
                disabled={busy}
                className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50"
              >
                {busy ? 'Converting…' : 'Convert'}
              </button>
            ) : <ReadOnly what="convert messages" />}
          </div>
        </Card>

        <div className="grid grid-cols-2 gap-4">
          <Card className="flex flex-col p-0">
            <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">
              {fromLabel} — input
            </div>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              spellCheck={false}
              className="min-h-[460px] flex-1 resize-none rounded-b-[var(--radius)] bg-transparent px-4 py-3 font-mono text-xs leading-relaxed text-text outline-none"
            />
          </Card>
          <Card className="flex flex-col p-0">
            <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">
              {toLabel} — output
            </div>
            {error ? (
              <div className="px-4 py-3 font-mono text-xs text-fail">{error}</div>
            ) : (
              <pre className="min-h-[460px] flex-1 overflow-auto whitespace-pre-wrap px-4 py-3 font-mono text-xs leading-relaxed text-muted">
                {output || 'Press Convert to transform the message.'}
              </pre>
            )}
          </Card>
        </div>
      </div>
    </Page>
  )
}
