# Reports and the Activity Log

## The database

prova keeps everything it needs to remember in one SQLite database, `prova.db`, in its data directory. The database engine is compiled into the binary; there is no database server to install, configure or patch.

It holds the accounts, the signed-in sessions, every scenario run with its full proof and certificate, every suite run, every load test summary, and the activity log. A run stored a year ago opens on the Proof screen exactly as it did the day it ran.

| Platform | Default data directory |
|---|---|
| Linux | `~/.config/prova` |
| macOS | `~/Library/Application Support/prova` |
| Windows | `%AppData%\prova` |

`-data DIR` or the `PROVA_DATA` environment variable chooses another. The database file is created readable by its owner only.

> Back up by copying `prova.db` while prova is stopped, or at any time with `sqlite3 prova.db ".backup copy.db"`. Copying the file while prova is writing to it can produce a copy that does not open.

## The Reports screen

Choose a period — 7, 30, 90 or 365 days — and the Reports screen shows, from the database:

- the totals of pass, fail and indeterminate, and the pass rate;
- verdicts by day;
- verdicts and latency (p50, p95) by target, by protocol, by scenario and by the person who ran them;
- every suite execution, with its counts and duration;
- every load test, with throughput and percentiles;
- the number of runs on which the two engines disagreed.

## CSV export

**Download CSV** on the Reports screen, or `GET /api/reports/runs.csv?days=N`, gives one row per verdict in the period:

| Column | Holds |
|---|---|
| `run` | the run id, which opens the proof at `/proof?run=ID` |
| `time_utc` | when it ran |
| `scenario_id`, `scenario`, `protocol`, `target` | what was driven where |
| `outcome`, `latency_ms` | the verdict and the target's response time |
| `finding`, `cites` | the deciding step of the proof and the evidence it cites |
| `certificate_engine` | who re-checked the proof |
| `second_engine_agrees` | whether the second engine reached the same verdict |
| `run_by`, `suite_run` | who ran it, and the suite execution it was part of |

A value that a spreadsheet would read as a formula is prefixed with an apostrophe, so opening the file cannot run anything.

## The activity log

The **Activity** screen, for administrators, lists who signed in and out and how, who created, changed or removed an account (including from the command line), and who ran which scenario, script, suite or load test, with its result.

## Retention

By default nothing is deleted. `-retain-days N` deletes runs, suite runs, load tests and activity older than N days, at start and then daily. Accounts and current sessions are never removed by retention.
