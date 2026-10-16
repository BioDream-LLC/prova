# The Command Line

The binary has four modes. With no subcommand it is the server.

| Command | Does |
|---|---|
| `prova [options]` | serve the web interface and API; the options are in [Server Options](#server-options) |
| `prova run …` | run a suite, scenario, load test or replay headless, for CI |
| `prova user …` | manage accounts |
| `prova manual …` | write this manual as one HTML file |

## prova run

`prova run` is the runner for CI/CD. It starts its own simulated targets, runs what it is asked to, prints a summary, and can write JUnit and JSON reports.

```
prova run --suite regression-iso20022 --junit results.xml --json results.json
prova run --scenario fix-over-limit --target fix-venue-bug
prova run --scenario pacs008-within-balance --target cbpr-uat --load 500 --concurrency 16
prova run --suite controls-iso20022 --record cassette.jsonl
prova run --replay cassette.jsonl
```

| Exit code | Means |
|---|---|
| 0 | every verdict passed |
| 1 | a verdict failed, or prova could not run |
| 2 | a verdict was indeterminate (unless `--allow-indeterminate`) |

Point your pipeline at the JUnit file and a failed proof fails the build. `prova run` does not use the database or need an account; it is meant to run in a build container.

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
