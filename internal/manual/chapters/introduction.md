# Introduction

prova tests financial systems by talking to them. It drives ISO 20022, ISO 8583 and FIX messages at a target — a payment host, a card switch, a gateway, a trading venue, an order management system — in scripted scenarios, watches what comes back, and decides whether the target behaved as it must.

This manual describes what prova does, every option it accepts, and — where a decision is not obvious — why it works the way it does.

## What prova is for

A test harness for a payment host usually answers one question: did the response look right? An `ACSC` status, a `DE39=00`, an execution report with `39=2`. That is necessary and nowhere near sufficient. A host that approves a payment larger than the account holds returns a perfectly well-formed approval. A switch that processes the same instruction twice returns two well-formed confirmations. Format validators pass both.

prova asks the second question: given what was known about the account, the instrument and the instruction, was that the right answer? It answers with a verdict — **pass**, **fail** or **indeterminate** — and, with every verdict, the proof that decides it.

## Why the verdict is a proof

Every verdict prova gives is decided by formal argumentation over the evidence of the exchange: what was sent, what was seeded at the target, what came back. The rules ("a payment exceeding available funds must be rejected") are arguments; the evidence attacks or supports them; the verdict is what survives under grounded semantics. The result is a graph you can read, a chain of cited steps, and a certificate from an independent re-check.

> A prova verdict is not an AI explanation. Nothing in prova guesses. The same exchange always gives the same verdict, and the proof shows exactly which rule and which evidence decided it.

That matters to the people who sign off on a release: a release manager, an auditor working to PCI DSS or SOX, a regulator asking how an automated decision was reached. "The test passed" is a claim. A proof is evidence.

## What is in it

Everything below is in the one binary. Each line names the chapter that covers it.

- 66 shipped scenarios and eleven suites across credit transfers, returns, cancellations and status requests; card authorisations, purchases, reversals and network messages; and orders, cancels, replaces and status requests — [suites](#suites-load-and-replay).
- Scenarios written in JavaScript or Lua, with standards-valid synthetic data — [scenarios](#scenarios-and-scripting).
- ISO 20022 (pacs.008 / pacs.002), ISO 8583 (0100 / 0110) and FIX 4.4 with a real session layer — [protocols](#protocols).
- MT103 ↔ pacs.008 transformation for the ISO 20022 migration — [protocols](#protocols).
- Verdicts decided by a casebook, re-decided by a second independent engine, and certified by a SAT-backed re-check — [verdicts](#verdicts-and-proofs).
- Regression and certification suites, load tests, and record & replay — [suites](#suites-load-and-replay).
- A database of every run and its proof, with reports, CSV export and an activity log — [reports](#reports-and-the-activity-log).
- Local, OpenID Connect and SAML sign-in with three roles — [accounts](#accounts-and-signing-in).
- A headless runner for CI that writes JUnit and fails the build on a failed proof — [the command line](#the-command-line).
- Built-in simulated targets, including ones with planted defects, so all of it works the moment it starts.

## How to read this manual

Read [Getting Started](#getting-started) first; it takes ten minutes and ends with a failed proof you caused on purpose. [Verdicts and Proofs](#verdicts-and-proofs) is the chapter that explains what prova is actually telling you, and is worth reading before relying on a verdict. The chapters after that can be read as needed. The last chapters are generated from the running code — the shipped scenarios, suites and every server option — so they cannot describe something that does not exist.

## Conventions

Commands are shown as you type them. `prova` means the binary, wherever you put it. A value in capitals, such as `NAME`, is one you supply.

> Notes like this one carry the things that bite: a default that surprises, an option that changes something else, a check that is easy to defeat by accident.
