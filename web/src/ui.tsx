import type { ReactNode } from 'react'
import clsx from 'clsx'

/** Page is the standard screen frame: a title row and a body. */
export function Page({ title, subtitle, actions, children }: {
  title: string
  subtitle?: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex h-full flex-col">
      <header className="flex items-start justify-between gap-4 border-b border-line px-7 py-5">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-text">{title}</h1>
          {subtitle && <p className="mt-0.5 text-sm text-muted">{subtitle}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-7">{children}</div>
    </div>
  )
}

/** Card is the elevated surface everything sits on. */
export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={clsx(
      'rounded-[var(--radius)] border border-line bg-surface/70 shadow-[0_1px_0_0_rgba(255,255,255,0.03)_inset,0_12px_30px_-18px_rgba(0,0,0,0.8)] backdrop-blur-sm',
      className,
    )}>
      {children}
    </div>
  )
}

/** Stat is a single headline metric — big tabular number, label, optional delta. */
export function Stat({ label, value, unit, delta, tone = 'neutral' }: {
  label: string
  value: string
  unit?: string
  delta?: string
  tone?: 'neutral' | 'pass' | 'fail' | 'indet'
}) {
  const toneClass = {
    neutral: 'text-text',
    pass: 'text-pass',
    fail: 'text-fail',
    indet: 'text-indet',
  }[tone]
  return (
    <Card className="p-5">
      <div className="text-xs font-medium uppercase tracking-wider text-faint">{label}</div>
      <div className="mt-2 flex items-baseline gap-1.5">
        <span className={clsx('tnum text-3xl font-semibold tracking-tight', toneClass)}>{value}</span>
        {unit && <span className="text-sm text-muted">{unit}</span>}
      </div>
      {delta && <div className="mt-1 text-xs text-muted tnum">{delta}</div>}
    </Card>
  )
}

/** Badge renders a verdict outcome with its semantic colour. */
export function Outcome({ value }: { value: 'pass' | 'fail' | 'indeterminate' }) {
  const map = {
    pass: ['Pass', 'text-pass', 'bg-pass/10', 'ring-pass/30'],
    fail: ['Fail', 'text-fail', 'bg-fail/10', 'ring-fail/30'],
    indeterminate: ['Indeterminate', 'text-indet', 'bg-indet/10', 'ring-indet/30'],
  }[value]
  return (
    <span className={clsx('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1', map[1], map[2], map[3])}>
      <span className={clsx('h-1.5 w-1.5 rounded-full', value === 'pass' ? 'bg-pass' : value === 'fail' ? 'bg-fail' : 'bg-indet')} />
      {map[0]}
    </span>
  )
}

/** Pill is a small neutral tag. */
export function Pill({ children }: { children: ReactNode }) {
  return <span className="rounded-md border border-line bg-surface2 px-2 py-0.5 text-xs text-muted">{children}</span>
}

/** fmtMs formats a latency in milliseconds: the simulators answer in well under one, so small values keep decimals. */
export function fmtMs(v: number | undefined): string {
  const x = v ?? 0
  if (x < 10) return x.toFixed(2)
  if (x < 100) return x.toFixed(1)
  return Math.round(x).toLocaleString()
}
