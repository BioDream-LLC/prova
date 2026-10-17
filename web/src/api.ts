// api.ts — the typed client for prova's HTTP API. Everything the interface shows comes through here;
// there is no mock data. Types mirror the Go JSON in internal/engine and internal/verdict.
import { useCallback, useEffect, useState } from 'react'

export type Outcome = 'pass' | 'fail' | 'indeterminate'
export type Label = 'in' | 'out' | 'undec'
export type AttackKind = 'rebut' | 'undermine' | 'undercut'

export interface Scenario {
  id: string
  name: string
  protocol: string
  message?: string
  description: string
  request: Record<string, string>
  context: Record<string, string>
  expect: string
}

export interface Target {
  name: string
  protocol: string
  addr: string
  note?: string
}

export interface Message {
  dir: 'out' | 'in'
  kind: string
  summary: string
  fields: Record<string, string>
  raw: string
  latencyMs?: number
}

export interface Arg {
  id: string
  claim: string
  cites?: string[]
}

export interface Attack {
  from: string
  to: string
  kind: AttackKind
}

export interface Step {
  claim: string
  status: Label
  because: string
  cites?: string[]
}

export interface CrossCheck {
  engine: string
  casebook: string
  fingerprint: string
  outcome: Outcome
  agrees: boolean
  readings: string[]
}

export interface Certificate {
  engine: string
  queryLabel: Label
  complete: boolean
  groundedVerified: boolean
  stableExists: boolean
  skeptical: boolean
  credulous: boolean
}

export interface Result {
  outcome: Outcome
  framework: { args: Arg[]; attacks: Attack[] }
  labels: Record<string, Label>
  chain: Step[]
  settles?: string[]
  certificate?: Certificate
  crossCheck?: CrossCheck
}

export interface Run {
  id: string
  scenarioId: string
  scenario: string
  target: string
  at: string
  outcome: Outcome
  latencyMs: number
  messages: Message[]
  result: Result
  by?: string
  suiteRun?: string
}

/** unauthorized is raised when the server says the session has ended; the auth gate shows the sign-in screen. */
export const AUTH_EVENT = 'prova:unauthorized'
function checkAuth(res: Response) {
  if (res.status === 401 && !res.url.includes('/api/auth/')) window.dispatchEvent(new Event(AUTH_EVENT))
}

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { headers: { Accept: 'application/json' } })
  checkAuth(res)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export interface Suite {
  id: string
  name: string
  description: string
  scenarios: string[]
  target: string
}

export interface SuiteResult {
  id: string
  name: string
  target: string
  at: string
  outcome: Outcome
  pass: number
  fail: number
  indeterminate: number
  durationMs: number
  runs: Run[]
}

export interface LoadResult {
  scenario: string
  target: string
  n: number
  concurrency: number
  durationMs: number
  throughput: number
  p50: number
  p95: number
  p99: number
  max: number
  errors: number
  outcomes: Record<string, number>
}

async function postJSON<T>(url: string, body: unknown, method = 'POST'): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  checkAuth(res)
  if (res.status === 204) return undefined as T
  const data = await res.json()
  if (!res.ok) throw new Error(data?.error ?? `${res.status}`)
  return data as T
}

export type Role = 'viewer' | 'operator' | 'admin'
export interface Me { user: string; role: Role; via: string; demo?: boolean }
export interface Providers {
  local: boolean
  oidc?: { label: string; start: string }
  saml?: { label: string; start: string }
  demo?: { name: string; password: string }
}
export interface Activity { at: string; actor: string; action: string; detail?: string }

export interface Tally { pass: number; fail: number; indeterminate: number }
export interface KeyTally extends Tally { key: string; label?: string; p50Ms: number; p95Ms: number }
export interface StoredSuiteRun extends Tally {
  id: string; suiteId: string; name: string; target: string; at: string; outcome: Outcome; durationMs: number; actor?: string
}
export interface StoredLoadRun {
  at: string; scenario: string; target: string; n: number; concurrency: number; throughput: number
  p50: number; p95: number; p99: number; max: number; errors: number; actor?: string
}
export interface Report {
  from: string; to: string; days: number; runs: number; totals: Tally
  byDay: (Tally & { day: string })[]
  byTarget: KeyTally[]; byProtocol: KeyTally[]; byScenario: KeyTally[]; byActor: KeyTally[]
  suites: StoredSuiteRun[]; loads: StoredLoadRun[]; engineDisagreements: number
}
export interface Account { name: string; role: Role; sso?: boolean; created: string }

export const authApi = {
  // Signed out, /api/auth/me answers 200 with no user (not a 401, which a browser would log as an error).
  me: async (): Promise<Me | null> => {
    const res = await fetch('/api/auth/me', { headers: { Accept: 'application/json' } })
    if (!res.ok) return null
    const me = (await res.json()) as Partial<Me>
    return me.user ? (me as Me) : null
  },
  providers: () => getJSON<Providers>('/api/auth/providers'),
  login: (name: string, password: string) => postJSON<Me>('/api/auth/login', { name, password }),
  logout: () => postJSON<void>('/api/auth/logout', {}),
  users: () => getJSON<Account[]>('/api/users'),
  setUser: (name: string, role: Role, sso: boolean) =>
    postJSON<{ name: string; role: Role; password?: string; sso?: boolean }>('/api/users', { name, role, sso }),
  removeUser: (name: string) => postJSON<void>(`/api/users/${encodeURIComponent(name)}`, {}, 'DELETE'),
  activity: () => getJSON<Activity[]>('/api/activity'),
}

export const api = {
  scenarios: () => getJSON<Scenario[]>('/api/scenarios'),
  targets: () => getJSON<Target[]>('/api/targets'),
  suites: () => getJSON<Suite[]>('/api/suites'),
  runs: () => getJSON<Run[]>('/api/runs'),
  run: (id: string) => getJSON<Run>(`/api/runs/${id}`),
  start: (scenario: string, target: string) => postJSON<Run>('/api/runs', { scenario, target }),
  runScript: (script: string, target: string, lang = 'js') => postJSON<Run>('/api/runs', { script, lang, target }),
  runSuite: (suite: string, target = '') => postJSON<SuiteResult>('/api/suites/run', { suite, target }),
  load: (scenario: string, target: string, n: number, concurrency: number) =>
    postJSON<LoadResult>('/api/load', { scenario, target, n, concurrency }),
  transform: (direction: 'mt2mx' | 'mx2mt', payload: string) =>
    postJSON<{ result: string; format: string }>('/api/transform', { direction, payload }),
  report: (days: number) => getJSON<Report>(`/api/reports/summary?days=${days}`),
}

/** useAsync runs loader on mount (and when deps change), exposing data/error/loading + reload. */
export function useAsync<T>(loader: () => Promise<T>, deps: unknown[] = []): {
  data: T | undefined
  error: string | undefined
  loading: boolean
  reload: () => void
} {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<string>()
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(loader, deps)

  useEffect(() => {
    let live = true
    setLoading(true)
    run()
      .then((d) => live && (setData(d), setError(undefined)))
      .catch((e) => live && setError(String(e.message ?? e)))
      .finally(() => live && setLoading(false))
    return () => { live = false }
  }, [run, tick])

  return { data, error, loading, reload: () => setTick((t) => t + 1) }
}
