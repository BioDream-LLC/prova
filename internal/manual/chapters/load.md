# Load and replay

A host that answers one message correctly may not answer ten thousand a minute correctly. prova's load runs send a scenario, or recorded traffic, at a target as hard or as steadily as asked, and judge every reply with the casebook, exactly as a single run is judged.

## Load runs

A load run sends one scenario over a number of workers. Each worker holds one connection for the whole run — for FIX, one session, with its sequence numbers — and reconnects only after an error, so the target sees the steady traffic a real counterparty sends.

| Setting | Meaning |
|---|---|
| requests | how many to send; or |
| seconds (`--duration`) | keep sending for this long; requests in flight at the end finish and count |
| rate | requests per second across all workers; 0 sends as fast as the target answers |
| connections (`--concurrency`) | workers, each with its own connection (at most 512) |

The summary gives throughput, the mean, p50, p95 and p99 latency and the maximum, the transport errors with the first one's text, the outcome of every judged reply, and a per-second timeline of requests, errors and p95 latency. On the **Suites** screen the timeline is drawn as a chart; from the command line:

```
prova run --scenario pacs008-within-balance --target cbpr-uat --load 10000 --concurrency 32
prova run --scenario fix-within-limit --target fix-venue --duration 60s --rate 2000 --concurrency 16
```

`prova run` exits non-zero when any request failed to get a reply or any reply failed its verdict. Load runs from the interface last at most ten minutes; use `prova run` for longer ones.

## Replaying production traffic

`prova import` turns traffic captured from a production or test system into a cassette, keeping the requests a tester sends and dropping the rest:

| Protocol | The capture |
|---|---|
| `fix` | a FIX log: QuickFIX and most engines write one message per line, with SOH, `\|` or `^A` between fields. Orders, cancels, replaces, status requests and mass cancels are kept, with their SendingTime. |
| `iso8583` | one message per line, as hex (any dialect, with or without the two-byte frame length) or, in the generic dialect, as the ASCII message. 0100, 0200, 0400 and 0800 are kept. |
| `iso20022` | XML documents, concatenated. The messages prova sends are kept, with their CreDtTm. |

```
prova import --protocol iso8583 --dialect visa --mask --context availableBalance=500.00 --out auth.jsonl dump.hex
prova run --replay-live auth.jsonl --target visa-switch --speed 1
```

`--mask` replaces card numbers (keeping the issuer's six-digit BIN and a valid Luhn digit), IBANs (keeping the country, with valid check digits), account ids and names with stand-ins derived from a hash of the original, so the same card masks the same way throughout and duplicates still look like duplicates. Masking changes the card number, so PIN blocks, MACs and chip cryptograms bound to it no longer verify: seed no keys for masked traffic. `--context` gives every recording the state the tester knows, so the verdicts can judge funds, limits and the rest.

`--replay-live` sends the recordings in order. `--speed 1` keeps the recorded spacing; `--speed 10` replays ten times faster; with `--rate` the pace is even instead. `--load N` loops over the recordings to send N requests. On the **Suites** screen, the Replay card does the same with a cassette or a raw capture, masking by default.

## Record and re-prove

`prova run --record cassette.jsonl` saves every exchange — what was sent, what the tester knew, what came back — and `prova run --replay cassette.jsonl` re-proves the cassette offline, with no target. After a change to the rules, the same traffic is judged again; a verdict that changed names the rule that changed it.
