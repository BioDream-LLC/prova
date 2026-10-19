import { useEffect, useMemo, useState } from 'react'
import { ShieldCheck, ShieldAlert, Download, CheckCircle2 } from 'lucide-react'
import { Page, Card, Pill } from './ui'
import { api, type ValidationReport, type ValidationExample, type Severity } from './api'

const PROFILES = ['', 'CBPR+'] as const

const sevTone: Record<Severity, string> = {
  error: 'text-fail ring-fail/30 bg-fail/10',
  warning: 'text-accent2 ring-accent2/30 bg-accent2/10',
  note: 'text-muted ring-line bg-surface2',
}

export function Validate() {
  const [examples, setExamples] = useState<ValidationExample[]>([])
  const [message, setMessage] = useState('')
  const [profile, setProfile] = useState<string>('CBPR+')
  const [report, setReport] = useState<ValidationReport>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.validateExamples().then((ex) => {
      setExamples(ex)
      const first = ex.find((e) => e.name === 'pacs.008-valid') ?? ex[0]
      if (first) setMessage(first.xml)
    }).catch((e) => setError(String(e)))
  }, [])

  const validate = async () => {
    setBusy(true)
    setError(undefined)
    try {
      setReport(await api.validate(message, profile))
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e))
      setReport(undefined)
    } finally {
      setBusy(false)
    }
  }

  const downloadSarif = () => {
    const url = `/api/validate?format=sarif`
    fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message, profile }) })
      .then((r) => r.blob())
      .then((b) => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(b)
        a.download = 'prova-findings.sarif'
        a.click()
        URL.revokeObjectURL(a.href)
      })
  }

  const counts = useMemo(() => {
    const c = { error: 0, warning: 0, note: 0 }
    report?.findings.forEach((f) => (c[f.severity] += 1))
    return c
  }, [report])

  const valid = report && counts.error === 0

  return (
    <Page
      title="Validate"
      subtitle="Check an ISO 20022 message against the official schema and the CBPR+ usage-guideline rules — every finding names the rule, the field and the fix"
      actions={<Pill>schema + profile</Pill>}
    >
      <div className="grid grid-cols-2 gap-4">
        <Card className="flex flex-col p-0">
          <div className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2.5">
            <span className="text-xs font-medium uppercase tracking-wider text-faint">Message</span>
            <select
              aria-label="Example message"
              className="ml-auto rounded-md border border-line bg-surface2 px-2 py-1 text-xs text-text"
              onChange={(e) => { const ex = examples.find((x) => x.name === e.target.value); if (ex) setMessage(ex.xml) }}
              defaultValue=""
            >
              <option value="" disabled>load an example…</option>
              {examples.map((ex) => <option key={ex.name} value={ex.name}>{ex.name}</option>)}
            </select>
            <select
              aria-label="Usage-guideline profile"
              className="rounded-md border border-line bg-surface2 px-2 py-1 text-xs text-text"
              value={profile}
              onChange={(e) => setProfile(e.target.value)}
            >
              {PROFILES.map((p) => <option key={p} value={p}>{p === '' ? 'schema only' : p}</option>)}
            </select>
            <button
              onClick={validate}
              disabled={busy || !message}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50"
            >
              {busy ? 'Validating…' : 'Validate'}
            </button>
          </div>
          <textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            spellCheck={false}
            placeholder="Paste an ISO 20022 message…"
            className="min-h-[520px] flex-1 resize-none rounded-b-[var(--radius)] bg-transparent px-4 py-3 font-mono text-xs leading-relaxed text-text outline-none"
          />
        </Card>

        <Card className="flex flex-col p-0">
          <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
            <span className="text-xs font-medium uppercase tracking-wider text-faint">Findings</span>
            {report && (
              <span className="flex items-center gap-2 text-xs text-muted">
                <span>{report.kind || 'unknown'}</span>
                {report.schema ? <Pill>schema applied</Pill> : <span className="text-accent2">no schema</span>}
                {report.profile && <Pill>{report.profile}</Pill>}
              </span>
            )}
            {report && (
              <button
                onClick={downloadSarif}
                title="Download findings as SARIF 2.1.0"
                className="ml-auto flex items-center gap-1.5 rounded-md border border-line bg-surface2 px-2.5 py-1 text-xs text-muted hover:text-text"
              >
                <Download className="h-3.5 w-3.5" /> SARIF
              </button>
            )}
          </div>
          <div className="min-h-[520px] flex-1 overflow-auto p-4">
            {error && <div className="font-mono text-xs text-fail">{error}</div>}
            {!report && !error && <div className="text-sm text-faint">Press Validate to check the message.</div>}
            {report && (
              <>
                <div className="mb-4 flex items-center gap-3">
                  {valid ? (
                    <span className="flex items-center gap-2 rounded-lg bg-pass/10 px-3 py-2 text-sm text-pass ring-1 ring-pass/30">
                      <CheckCircle2 className="h-4 w-4" /> Valid — no errors
                    </span>
                  ) : (
                    <span className="flex items-center gap-2 rounded-lg bg-fail/10 px-3 py-2 text-sm text-fail ring-1 ring-fail/30">
                      <ShieldAlert className="h-4 w-4" /> {counts.error} error{counts.error === 1 ? '' : 's'}
                    </span>
                  )}
                  {counts.warning > 0 && <span className="text-xs text-accent2">{counts.warning} warning{counts.warning === 1 ? '' : 's'}</span>}
                  {counts.note > 0 && <span className="text-xs text-muted">{counts.note} note{counts.note === 1 ? '' : 's'}</span>}
                </div>
                {report.findings.length === 0 ? (
                  <div className="flex items-center gap-2 text-sm text-muted">
                    <ShieldCheck className="h-4 w-4 text-pass" /> The message satisfies the schema{report.profile ? ` and the ${report.profile} profile` : ''}.
                  </div>
                ) : (
                  <ul className="flex flex-col gap-2">
                    {report.findings.map((f, i) => (
                      <li key={i} className={`rounded-lg px-3 py-2.5 text-sm ring-1 ${sevTone[f.severity]}`}>
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-[11px] uppercase tracking-wide">{f.severity}</span>
                          <span className="font-mono text-[11px] text-muted">{f.rule}</span>
                        </div>
                        <div className="mt-1 text-text">{f.message}</div>
                        <div className="mt-0.5 font-mono text-[11px] text-faint">{f.path}</div>
                        {f.fix && <div className="mt-1 text-xs text-muted">fix: {f.fix}</div>}
                      </li>
                    ))}
                  </ul>
                )}
              </>
            )}
          </div>
        </Card>
      </div>
    </Page>
  )
}
