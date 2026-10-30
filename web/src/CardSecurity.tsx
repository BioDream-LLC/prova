import { useEffect, useState, type ReactNode } from 'react'
import { KeyRound, Lock, Fingerprint, Cpu, ShieldCheck, Server, AlertTriangle } from 'lucide-react'
import { Page, Card, Pill } from './ui'
import { api, type TestKeys, type HSMStatus, type CryptoRequest } from './api'

type Tab = 'pin' | 'mac' | 'arqc' | 'pvv' | 'hsm'

const tabs: { id: Tab; label: string; icon: typeof KeyRound }[] = [
  { id: 'pin', label: 'PIN block', icon: Lock },
  { id: 'mac', label: 'MAC', icon: Fingerprint },
  { id: 'arqc', label: 'Cryptogram', icon: Cpu },
  { id: 'pvv', label: 'PVV / CVV', icon: ShieldCheck },
  { id: 'hsm', label: 'HSM', icon: Server },
]

const inputCls = 'w-full rounded-lg border border-line bg-surface/60 px-2.5 py-1.5 font-mono text-xs text-text outline-none focus:border-accent/60'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[11px] uppercase tracking-wider text-faint">{label}</span>
      {children}
    </label>
  )
}

function Text({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <Field label={label}><input aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} className={inputCls} spellCheck={false} /></Field>
}

function Choice({ label, value, onChange, options }: { label: string; value: string; onChange: (v: string) => void; options: [string, string][] }) {
  return (
    <Field label={label}>
      <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} className={inputCls}>
        {options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
      </select>
    </Field>
  )
}

/** useCalc posts one calculation and keeps its answer or error. */
function useCalc() {
  const [out, setOut] = useState<Record<string, string | number | boolean>>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const run = async (req: CryptoRequest) => {
    setBusy(true)
    setError(undefined)
    try {
      setOut(await api.cardCrypto(req))
    } catch (e) {
      setOut(undefined)
      setError(String(e instanceof Error ? e.message : e))
    } finally {
      setBusy(false)
    }
  }
  return { out, error, busy, run }
}

function Result({ out, error }: { out?: Record<string, string | number | boolean>; error?: string }) {
  if (error) return <div className="mt-4 rounded-lg bg-fail/10 px-3 py-2 font-mono text-xs text-fail ring-1 ring-fail/30">{error}</div>
  if (!out) return null
  return (
    <dl className="mt-4 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 rounded-lg bg-surface2/50 px-4 py-3 ring-1 ring-line">
      {Object.entries(out).map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-[11px] uppercase tracking-wider text-faint">{k}</dt>
          <dd className={`break-all font-mono text-xs ${v === true ? 'text-pass' : v === false ? 'text-fail' : 'text-text'}`}>{String(v)}</dd>
        </div>
      ))}
    </dl>
  )
}

function Go({ busy, onClick, children }: { busy: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button onClick={onClick} disabled={busy} className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50">
      {busy ? 'Working…' : children}
    </button>
  )
}

function PINPanel({ keys }: { keys: TestKeys }) {
  const c = useCalc()
  const [format, setFormat] = useState('0')
  const [pin, setPIN] = useState('1234')
  const [pan, setPAN] = useState('4111111111111111')
  const [key, setKey] = useState(keys.pinKey)
  const [block, setBlock] = useState('')
  const fmt = (f: string) => { setFormat(f); setKey(f === '4' ? keys.pinKeyAES : keys.pinKey) }
  return (
    <>
      <div className="grid grid-cols-2 gap-3">
        <Choice label="Format (ISO 9564)" value={format} onChange={fmt} options={[['0', '0 — PIN XOR PAN'], ['1', '1 — no PAN'], ['3', '3 — random A-F fill'], ['4', '4 — AES']]} />
        <Text label={format === '4' ? 'PIN key (AES)' : 'PIN key (Triple DES)'} value={key} onChange={setKey} />
        <Text label="PIN" value={pin} onChange={setPIN} />
        <Text label="Card number" value={pan} onChange={setPAN} />
        <Text label="PIN block (to decipher)" value={block} onChange={setBlock} />
      </div>
      <div className="mt-4 flex gap-2">
        <Go busy={c.busy} onClick={() => c.run({ op: 'pinblock', format, key, pin, pan }).then(() => undefined)}>Encipher</Go>
        <button onClick={() => c.run({ op: 'decipher', format, key, block, pan })} disabled={c.busy || !block}
          className="rounded-lg border border-line bg-surface2 px-4 py-1.5 text-sm text-muted hover:text-text disabled:opacity-40">Decipher</button>
      </div>
      <Result out={c.out} error={c.error} />
      {typeof c.out?.block === 'string' && (
        <button onClick={() => setBlock(String(c.out!.block))} className="mt-2 text-xs text-accent hover:underline">use this block to decipher</button>
      )}
    </>
  )
}

function MACPanel({ keys }: { keys: TestKeys }) {
  const c = useCalc()
  const [alg, setAlg] = useState('retail')
  const [key, setKey] = useState(keys.macKey)
  const [f, setF] = useState({ pan: '4111111111111111', amount: '90.00', currency: 'EUR', stan: '000120', terminal: 'TERM0001' })
  const set = (k: keyof typeof f) => (v: string) => setF({ ...f, [k]: v })
  return (
    <>
      <div className="grid grid-cols-2 gap-3">
        <Choice label="Algorithm" value={alg} onChange={(v) => { setAlg(v); setKey(v === 'cmac' ? keys.macKeyAES : keys.macKey) }}
          options={[['retail', 'ISO 9797-1 alg. 3 (retail, ANSI X9.19)'], ['cbc', 'ISO 9797-1 alg. 1 (CBC-MAC)'], ['cmac', 'AES-CMAC (SP 800-38B)']]} />
        <Text label="MAC key" value={key} onChange={setKey} />
        <Text label="Card number (DE2)" value={f.pan} onChange={set('pan')} />
        <Text label="Amount (DE4)" value={f.amount} onChange={set('amount')} />
        <Text label="Currency (DE49)" value={f.currency} onChange={set('currency')} />
        <Text label="STAN (DE11)" value={f.stan} onChange={set('stan')} />
        <Text label="Terminal (DE41)" value={f.terminal} onChange={set('terminal')} />
      </div>
      <p className="mt-3 text-xs text-muted">prova MACs the card number, processing code, amount, currency, STAN and terminal id, each followed by a 0x1C separator — a field-selection MAC as ANSI X9.19 allows.</p>
      <div className="mt-3"><Go busy={c.busy} onClick={() => c.run({ op: 'mac', algorithm: alg, key, ...f })}>Compute MAC</Go></div>
      <Result out={c.out} error={c.error} />
    </>
  )
}

function ARQCPanel({ keys }: { keys: TestKeys }) {
  const c = useCalc()
  const [ver, setVer] = useState(keys.versions[0] ?? 'visa-cvn10')
  const [imk, setIMK] = useState(keys.imk)
  const [f, setF] = useState({ pan: '4111111111111111', psn: '01', amount: '90.00', currency: '978', date: '261015', un: '1A2B3C4D', atc: '0042' })
  const [icc, setICC] = useState('')
  const set = (k: keyof typeof f) => (v: string) => setF({ ...f, [k]: v })
  const pick = (v: string) => {
    setVer(v)
    const card = keys.cards.find((x) => x.Cryptogram === v)
    if (card) setF({ ...f, pan: card.PAN })
  }
  return (
    <>
      <div className="grid grid-cols-2 gap-3">
        <Choice label="Cryptogram version" value={ver} onChange={pick}
          options={keys.versions.map((v) => [v, { 'visa-cvn10': 'Visa CVN 10', 'visa-cvn18': 'Visa CVN 18', 'mc-cvn16': 'Mastercard CVN 16' }[v] ?? v])} />
        <Text label="Issuer master key" value={imk} onChange={setIMK} />
        <Text label="Card number" value={f.pan} onChange={set('pan')} />
        <Text label="PAN sequence number" value={f.psn} onChange={set('psn')} />
        <Text label="Amount" value={f.amount} onChange={set('amount')} />
        <Text label="Currency (numeric)" value={f.currency} onChange={set('currency')} />
        <Text label="Date (YYMMDD)" value={f.date} onChange={set('date')} />
        <Text label="Unpredictable number" value={f.un} onChange={set('un')} />
        <Text label="ATC" value={f.atc} onChange={set('atc')} />
      </div>
      <div className="mt-4 flex gap-2">
        <Go busy={c.busy} onClick={() => c.run({ op: 'arqc', version: ver, key: imk, ...f })}>Build chip data</Go>
        <button onClick={() => c.run({ op: 'verify-arqc', version: ver, key: imk, pan: f.pan, psn: f.psn, icc })} disabled={c.busy || !icc}
          className="rounded-lg border border-line bg-surface2 px-4 py-1.5 text-sm text-muted hover:text-text disabled:opacity-40">Verify chip data</button>
      </div>
      <div className="mt-3"><Text label="Chip data (DE55) to verify" value={icc} onChange={setICC} /></div>
      <Result out={c.out} error={c.error} />
      {typeof c.out?.icc === 'string' && (
        <button onClick={() => setICC(String(c.out!.icc))} className="mt-2 text-xs text-accent hover:underline">verify this chip data</button>
      )}
    </>
  )
}

function PVVPanel() {
  const c = useCalc()
  const [kind, setKind] = useState('pvv')
  const [key, setKey] = useState('0123456789ABCDEFFEDCBA9876543210')
  const [f, setF] = useState({ pan: '4111111111111111', pin: '1234', pvki: '1', expiry: '3012', service: '101' })
  const set = (k: keyof typeof f) => (v: string) => setF({ ...f, [k]: v })
  return (
    <>
      <div className="grid grid-cols-2 gap-3">
        <Choice label="Value" value={kind} onChange={setKind} options={[['pvv', 'Visa PVV'], ['cvv', 'CVV / CVC'], ['kcv', 'Key check value']]} />
        <Text label={kind === 'pvv' ? 'PVK pair' : kind === 'cvv' ? 'CVK pair' : 'Key'} value={key} onChange={setKey} />
        {kind !== 'kcv' && <Text label="Card number" value={f.pan} onChange={set('pan')} />}
        {kind === 'pvv' && <><Text label="PIN" value={f.pin} onChange={set('pin')} /><Text label="PVK index" value={f.pvki} onChange={set('pvki')} /></>}
        {kind === 'cvv' && <><Text label="Expiry (YYMM)" value={f.expiry} onChange={set('expiry')} /><Text label="Service code" value={f.service} onChange={set('service')} /></>}
      </div>
      <div className="mt-4"><Go busy={c.busy} onClick={() => c.run({ op: kind, key, ...f })}>Compute</Go></div>
      <Result out={c.out} error={c.error} />
    </>
  )
}

function HSMPanel() {
  const [st, setSt] = useState<HSMStatus>()
  const [busy, setBusy] = useState(false)
  const check = () => { setBusy(true); api.hsm().then(setSt).catch((e) => setSt({ configured: true, error: String(e) })).finally(() => setBusy(false)) }
  useEffect(check, [])
  return (
    <>
      <p className="text-sm text-muted">prova speaks the Thales payShield host commands: <span className="font-mono">NC</span> diagnostics, <span className="font-mono">CA</span> PIN translation, <span className="font-mono">EC</span> PIN verification against a Visa PVV and <span className="font-mono">CY</span> CVV verification. Start the server with <span className="font-mono">-hsm host:port</span> (and <span className="font-mono">-hsm-header</span>) to check one here, or use <span className="font-mono">prova hsm</span> from a terminal.</p>
      <div className="mt-4 flex items-center gap-3">
        <Go busy={busy} onClick={check}>Check the HSM</Go>
        {st && !st.configured && <span className="text-sm text-faint">No HSM is configured on this server.</span>}
      </div>
      {st?.configured && <Result out={Object.fromEntries(Object.entries(st).filter(([k]) => k !== 'configured')) as Record<string, string>} />}
    </>
  )
}

export function CardSecurity() {
  const [tab, setTab] = useState<Tab>('pin')
  const [keys, setKeys] = useState<TestKeys>()
  const [error, setError] = useState<string>()
  useEffect(() => { api.testKeys().then(setKeys).catch((e) => setError(String(e))) }, [])
  return (
    <Page
      title="Card security"
      subtitle="Build and check PIN blocks, MACs and chip cryptograms with the published test keys — the same arithmetic both verdict engines use to prove a switch's decision"
      actions={<Pill>test keys only</Pill>}
    >
      <div className="grid grid-cols-[1fr_20rem] gap-4">
        <Card className="p-0">
          <div role="tablist" className="flex gap-1 border-b border-line px-3 pt-2">
            {tabs.map(({ id, label, icon: Icon }) => (
              <button key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)}
                className={`flex items-center gap-1.5 rounded-t-lg px-3 py-2 text-sm transition-colors ${tab === id ? 'bg-surface2 text-text' : 'text-muted hover:text-text'}`}>
                <Icon className="h-4 w-4" /> {label}
              </button>
            ))}
          </div>
          <div className="p-5">
            {error && <div className="text-sm text-fail">{error}</div>}
            {keys && tab === 'pin' && <PINPanel keys={keys} />}
            {keys && tab === 'mac' && <MACPanel keys={keys} />}
            {keys && tab === 'arqc' && <ARQCPanel keys={keys} />}
            {tab === 'pvv' && <PVVPanel />}
            {tab === 'hsm' && <HSMPanel />}
          </div>
        </Card>
        <div className="flex flex-col gap-4">
          <Card className="p-4">
            <div className="mb-2 flex items-center gap-2 text-sm font-medium text-text"><KeyRound className="h-4 w-4 text-accent2" /> Published test keys</div>
            {keys ? (
              <dl className="flex flex-col gap-2 text-xs">
                {([['PIN key', keys.pinKey], ['PIN key, AES', keys.pinKeyAES], ['MAC key', keys.macKey], ['MAC key, AES', keys.macKeyAES], ['Issuer master key', keys.imk]] as const).map(([l, v]) => (
                  <div key={l}><dt className="text-faint">{l}</dt><dd className="break-all font-mono text-text">{v}</dd></div>
                ))}
              </dl>
            ) : <div className="text-xs text-faint">loading…</div>}
          </Card>
          <Card className="p-4">
            <div className="mb-2 text-sm font-medium text-text">Test cards</div>
            <table className="w-full text-xs">
              <tbody>
                {keys?.cards.map((c) => (
                  <tr key={c.PAN} className="border-t border-line/60"><td className="py-1.5 font-mono text-text">{c.PAN}</td><td className="font-mono text-muted">{c.PIN}</td><td className="text-right text-faint">{c.Cryptogram}</td></tr>
                ))}
              </tbody>
            </table>
          </Card>
          <Card className="flex gap-2 p-4 text-xs text-muted">
            <AlertTriangle className="h-4 w-4 shrink-0 text-accent2" />
            These are test keys, published so anyone can reproduce prova's PIN blocks, MACs and cryptograms. Never enter a production key here.
          </Card>
        </div>
      </div>
    </Page>
  )
}
