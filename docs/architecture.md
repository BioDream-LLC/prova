# prova architecture

One Go binary serves the web interface, the API, the manual and the database. An optional C++ companion,
`prova-proof`, re-checks every proof with a SAT solver.

```
 browser ──HTTPS──► authweb.Guard ──► API handlers ──► engine ──► adapter (iso20022 | iso8583 | fix) ──► target
                     (sessions,          │                │
                      roles, CSRF)       │                ├──► verdict casebook ──► Result (graph, chain)
                                         │                ├──► qcasebook (quorum's compiled rulebook) ──► CrossCheck
                                         │                └──► proof.Verify ──► prova-proof (C++, SAT) | builtin-go
                                         └──► store (SQLite: accounts, sessions, runs, suites, loads, activity)
```

## Packages

| Package | Does |
|---|---|
| `cmd/prova` | the server, `prova run` (CI), `prova user`, `prova manual`; TLS; reports API |
| `internal/engine` | runs scenarios, suites, load tests and replays; keeps recent runs; persists through a `Store` |
| `internal/adapter` | the protocol SPI: `Connect`, `Encode`, `Send`, `Await` |
| `internal/iso20022`, `iso8583`, `fix` | the three wire protocols, each with a simulated target |
| `internal/mt` | MT103 ↔ pacs.008 |
| `internal/verdict` | the casebook: rules that build an argumentation framework from an exchange |
| `internal/af` | grounded semantics, labelling and the cited proof chain |
| `internal/qcasebook` | the second engine: quorum's cbc-compiled rulebook and the binding that reads an exchange |
| `internal/proof` | the bridge to `prova-proof`, and the in-process certificate when it is absent |
| `internal/script` | sandboxed JavaScript (goja) and Lua (gopher-lua) scenario scripts |
| `internal/synth` | standards-valid synthetic data |
| `internal/store`, `internal/sqlitedb` | the embedded SQLite database |
| `internal/auth`, `internal/authweb` | accounts, sessions, roles; local, OpenID Connect and SAML sign-in |
| `internal/oidc`, `internal/saml` | the identity-provider clients, standard library only |
| `internal/manual` | the reference manual: embedded chapters rendered to one HTML page |
| `internal/web` | the built interface, embedded |
| `proof/` | `prova-proof`, the C++ companion |
| `web/` | the React interface source |

## The two seams

**Protocols.** Every adapter normalises a response to the same decision keys — `decision` (`accepted` /
`rejected`) and `rejectReason` — so the casebook judges every protocol with the same rules. Adding a protocol is an
adapter and a simulator; nothing above the adapter changes.

**Verdicts.** The casebook produces an argumentation framework, not a boolean. Everything after it — the grounded
labelling, the second engine, the certificate — works on that framework, so each check is independent of how the
framework was built.

## Data

`prova.db` (WAL mode, one writer and a pool of readers) holds accounts, sessions keyed by the SHA-256 of their token,
every run with its full JSON (messages, framework, chain, certificate, cross-check), suite and load summaries, and the
activity log. The engine keeps the most recent 500 runs in memory for the interface and reads older ones from the
database on demand.
