# Suites, Load and Replay

## Suites

A suite runs a set of scenarios as one batch against a target and aggregates the result. The aggregate is **fail** if any scenario failed, **indeterminate** if any was indeterminate and none failed, and **pass** otherwise. Each scenario's run is stored with its proof and linked to the suite execution, so a failed certification run can be reopened months later exactly as it was.

prova ships regression packs for ISO 20022, ISO 8583 and FIX, a CBPR+ certification pack and an ISO 20022 controls pack; they are listed in the [Suite Reference](#suite-reference). Run one from the **Suites** screen, choosing another target if needed, or from CI:

```
prova run --suite regression-iso20022 --junit results.xml
```

## Load tests

A load test runs one scenario many times across a chosen concurrency and reports throughput and latency percentiles — p50, p95, p99 and the maximum — with the spread of verdicts.

```
prova run --scenario pacs008-within-balance --target cbpr-uat --load 500 --concurrency 16
```

Every request in a load test is still judged. A host that is correct one request at a time and approves an over-balance payment under concurrency shows up as failures in the spread, which is the defect a functional suite cannot find. The individual requests are not stored — a load test can be large — but its summary is, and appears on the Reports screen.

## Record and replay

prova can capture a run or a whole suite as a **cassette** and re-prove it later with no target at all:

```
prova run --suite regression-iso20022 --record cassette.jsonl
prova run --replay cassette.jsonl
```

A cassette is JSON Lines: one exchange per line, with the request, the seeded context and the exact normalised response. Replay re-judges each exchange against the current casebook and produces a full, certified proof. That turns captured traffic into a regression pack, and lets a change to the rules be checked against yesterday's responses without touching a host.

> A cassette contains whatever the request and response contained. Treat one recorded from production traffic as production data.
