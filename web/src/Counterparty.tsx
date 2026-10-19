import { useState } from 'react'
import { Page, Card, Pill } from './ui'
import { api } from './api'

// Counterparty lets a user act as the far side of the exchange: dictate the reply a payment host,
// card switch or trading venue would send, and see the exact wire message prova would read back and
// the normalized fields it decides on. It is the manual twin of the built-in simulators.
const PROTOCOLS: { id: string; label: string; message: string; acceptLabel: string; rejectLabel: string; reasonHint: string }[] = [
  { id: 'iso20022', label: 'ISO 20022 host', message: 'pacs.002', acceptLabel: 'accept (ACSP)', rejectLabel: 'reject (RJCT)', reasonHint: 'AM04' },
  { id: 'iso8583', label: 'ISO 8583 switch', message: '0110', acceptLabel: 'approve (DE39=00)', rejectLabel: 'decline', reasonHint: '51' },
  { id: 'fix', label: 'FIX venue', message: '8', acceptLabel: 'fill (OrdStatus=2)', rejectLabel: 'reject', reasonHint: '3' },
]

export function Counterparty() {
  const [protocol, setProtocol] = useState('iso20022')
  const [decision, setDecision] = useState<'accept' | 'reject'>('accept')
  const [reason, setReason] = useState('')
  const [out, setOut] = useState<{ raw: string; fields: Record<string, string> }>()
  const [error, setError] = useState<string>()
  const p = PROTOCOLS.find((x) => x.id === protocol)!

  const compose = async () => {
    setError(undefined)
    try {
      setOut(await api.counterparty(protocol, '', decision, decision === 'reject' ? reason : ''))
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
      setOut(undefined)
    }
  }

  return (
    <Page
      title="Counterparty"
      subtitle="Act as the far side of the exchange: dictate the reply a host, switch or venue would send, and see the exact wire message and the fields prova reads"
      actions={<Pill>response composer</Pill>}
    >
      <div className="grid grid-cols-2 gap-4">
        <Card className="flex flex-col gap-4 p-4">
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wider text-faint">Counterparty</div>
            <div className="flex gap-2">
              {PROTOCOLS.map((x) => (
                <button
                  key={x.id}
                  onClick={() => { setProtocol(x.id); setOut(undefined) }}
                  className={`rounded-lg px-3 py-1.5 text-sm ring-1 ${protocol === x.id ? 'bg-accent/15 text-text ring-accent/30' : 'bg-surface2 text-muted ring-line hover:text-text'}`}
                >
                  {x.label}
                </button>
              ))}
            </div>
          </div>
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wider text-faint">Decision</div>
            <div className="flex gap-2">
              <button
                onClick={() => setDecision('accept')}
                className={`rounded-lg px-3 py-1.5 text-sm ring-1 ${decision === 'accept' ? 'bg-pass/15 text-pass ring-pass/30' : 'bg-surface2 text-muted ring-line hover:text-text'}`}
              >
                {p.acceptLabel}
              </button>
              <button
                onClick={() => setDecision('reject')}
                className={`rounded-lg px-3 py-1.5 text-sm ring-1 ${decision === 'reject' ? 'bg-fail/15 text-fail ring-fail/30' : 'bg-surface2 text-muted ring-line hover:text-text'}`}
              >
                {p.rejectLabel}
              </button>
            </div>
          </div>
          {decision === 'reject' && (
            <div>
              <div className="mb-1.5 text-xs font-medium uppercase tracking-wider text-faint">Reason code</div>
              <input
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder={`e.g. ${p.reasonHint}`}
                className="w-40 rounded-md border border-line bg-surface2 px-2 py-1 font-mono text-sm text-text outline-none focus:ring-1 focus:ring-accent"
              />
            </div>
          )}
          <button onClick={compose} className="w-fit rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent/90">
            Compose reply
          </button>
        </Card>

        <Card className="flex flex-col p-0">
          <div className="border-b border-line px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-faint">
            {p.message} — wire message
          </div>
          <div className="min-h-[360px] flex-1 overflow-auto p-4">
            {error && <div className="font-mono text-xs text-fail">{error}</div>}
            {!out && !error && <div className="text-sm text-faint">Compose a reply to see the wire message.</div>}
            {out && (
              <>
                <pre className="mb-4 whitespace-pre-wrap break-all rounded-lg bg-surface2 p-3 font-mono text-xs leading-relaxed text-muted">
                  {out.raw.split('\x01').join('|')}
                </pre>
                <div className="text-xs font-medium uppercase tracking-wider text-faint">Normalized fields</div>
                <table className="mt-2 w-full text-sm">
                  <tbody>
                    {Object.entries(out.fields).map(([k, v]) => (
                      <tr key={k} className="border-b border-line/60">
                        <td className="py-1 pr-4 font-mono text-xs text-accent">{k}</td>
                        <td className="py-1 font-mono text-xs text-text">{v}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            )}
          </div>
        </Card>
      </div>
    </Page>
  )
}
