# The Command Line

With no subcommand the binary is the server.

| Command | Does |
|---|---|
| `prova [options]` | serve the web interface and API; the options are in [Server Options](#server-options) |
| `prova run …` | run a suite, scenario, load test or replay headless, for CI |
| `prova validate …` | validate an ISO 20022 message against the schema, a profile and a usage-guideline schema |
| `prova catalogue …` | list the ISO 20022 catalogue; write a message's sample, broken variant or schema |
| `prova guideline …` | manage the usage-guideline library |
| `prova import …` | turn captured traffic into a cassette to replay |
| `prova hsm …` | check an HSM that speaks the payShield host commands |
| `prova user …` | manage accounts |
| `prova manual …` | write this manual as one HTML file |

## prova run

`prova run` is the runner for CI/CD. It starts its own simulated targets, runs what it is asked to, prints a summary, and can write JUnit and JSON reports.

```
prova run --suite regression-iso20022 --junit results.xml --json results.json
prova run --scenario fix-over-limit --target fix-venue-bug
prova run --scenario pacs008-within-balance --target cbpr-uat --load 500 --concurrency 16
prova run --scenario fix-within-limit --target fix-venue --duration 60s --rate 2000 --concurrency 16
prova run --suite controls-iso20022 --record cassette.jsonl
prova run --replay cassette.jsonl                       # re-prove offline
prova run --replay-live capture.jsonl --target card-switch --speed 1
```

`--load N` or `--duration` makes it a load run, paced by `--rate`, over `--concurrency` connections; `--replay-live` sends a cassette to a target. See [Load and replay](#load-and-replay).

| Exit code | Means |
|---|---|
| 0 | every verdict passed |
| 1 | a verdict failed, or prova could not run |
| 2 | a verdict was indeterminate (unless `--allow-indeterminate`) |

Point your pipeline at the JUnit file and a failed proof fails the build. `prova run` does not use the database or need an account; it is meant to run in a build container.

## prova validate

`prova validate` checks an ISO 20022 message against the bundled schema, a profile (`--profile CBPR+`, `SEPA`, `SEPA-Inst` or `FedNow`) and, with `--schema`, a usage-guideline schema. It prints the findings (or SARIF with `--sarif`) and exits non-zero on an error, so it gates a build.

```
prova validate payment.xml
prova validate --profile SEPA payment.xml
prova validate --schema guideline.xsd payment.xml
prova validate --guideline tch-rtp/pacs.008.001.08 payment.xml
prova validate --profile CBPR+ --sarif payment.xml > findings.sarif
cat payment.xml | prova validate -
```

See [Validating messages](#validating-messages).

## prova catalogue and prova guideline

See [Validating messages](#validating-messages) for both.

## prova import

```
prova import --protocol fix --out orders.jsonl quickfix.log
prova import --protocol iso8583 --dialect visa --mask --context availableBalance=500.00 --out auth.jsonl dump.hex
prova import --protocol iso20022 --out payments.jsonl payments.xml
```

Reads a FIX log, an ISO 8583 dump or ISO 20022 files, keeps the requests, optionally masks card numbers, IBANs, accounts and names, and writes a cassette. See [Load and replay](#load-and-replay).

## prova hsm

```
prova hsm --addr HOST:PORT [--header H] diagnostics
prova hsm --addr HOST:PORT verify-pin ZPK PVK-PAIR PIN-BLOCK FORMAT PAN PVKI PVV
prova hsm --addr HOST:PORT verify-cvv CVK CVV PAN EXPIRY SERVICE-CODE
prova hsm --addr HOST:PORT translate-pin TPK ZPK PIN-BLOCK PAN
```

Keys are given as the HSM holds them, encrypted under its LMK. It exits 0 when the HSM verifies, 1 on a verification failure or an error. See [Card security](#card-security).

## prova user

```
prova user [-data DIR] list
prova user [-data DIR] add NAME ROLE      # viewer | operator | admin; prints the password once
prova user [-data DIR] reset NAME         # a new password; the account's sessions end
prova user [-data DIR] sso NAME ROLE      # signs in only through OpenID Connect or SAML
prova user [-data DIR] remove NAME
```

It works on the same database as the server and may be used while the server runs; the change applies at the next request. Every change is recorded in the activity log as made from the command line.

## prova manual

```
prova manual -out prova-manual.html
```

Writes this manual — including the reference chapters generated from the binary — as one self-contained HTML file. Print it from a browser for a PDF; the stylesheet has print rules, so chapters start on new pages and examples are not split across them. `make manual` does both.
