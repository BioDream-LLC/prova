# The Web Interface

Everything prova does can be done from the web interface. A feature that can only be reached from a terminal is treated as a defect. The interface is served by the binary itself and needs nothing else.

## The sidebar

The left rail lists every screen. At its foot is your account: your name, your role and how you signed in, and the sign-out button. The **Users** and **Activity** screens appear only for administrators. **Manual** opens this manual in a new tab, served by the same server, so it always matches the version you are running.

## Dashboard

The dashboard is the state of testing at a glance: how many verdicts passed, failed and were indeterminate, the latency of the targets, and the most recent runs. Click any run to open its proof.

## Scenarios

The scenario library and the script editor. Filter the library by protocol or search it, and choose a scenario to see the message it sends, its request, the context it seeds, and what it expects; choose a target — only those that speak the scenario's protocol are offered, and a scenario written for one target starts on it — and run it. The editor takes JavaScript or Lua (see [Scenarios and Scripting](#scenarios-and-scripting)) and runs what you write against the chosen target.

## Suites

The shipped regression and certification packs, each with a default target that can be changed before running. A suite's result is an aggregate verdict and a row per scenario, each linking to its proof. The **Load test** panel runs one scenario by count or for a number of seconds, at a rate, over a number of connections, and draws a per-second chart. The **Replay traffic** panel sends a cassette or a raw capture to a target, masking cardholder data by default. See [Load and replay](#load-and-replay).

## Messages

The wire exchange of a run, message by message: the fields as prova decoded them and the raw bytes as they crossed the connection. For FIX this includes the session messages; for ISO 8583 the bitmap and data elements.

## Transform

Eight MT ↔ MX pairs: choose one to load its sample, convert, and swap direction to convert back. Each result lists what the mapping could not carry. See [Protocols](#protocols).

## Validate

Paste or load an ISO 20022 message and check it against the official XML schema, a usage-guideline profile (CBPR+, SEPA, SEPA Inst or FedNow) and, if you add one, your own usage-guideline schema. Every finding names the rule, the field it points at and the fix. Bundled examples show both a clean pass and real findings, and the findings export as SARIF 2.1.0 for code scanning. The same engine backs `prova validate` on the command line. See [Validating messages](#validating-messages).

## Card security

PIN blocks, MACs, chip cryptograms, PVVs, CVVs and key check values, built and checked with the published test keys shown beside them, and the HSM check when the server has one. See [Card security](#card-security).

## Counterparty

Act as the far side of the exchange: choose the reply a payment host, card switch or trading venue would send — accept or reject, with a reason code — and see the exact wire message prova would read back and the normalized fields it decides on. It is the manual twin of the built-in simulators.

## Proof

The heart of prova. For the selected run it shows the verdict, the argumentation graph that decided it, the cited proof chain, what evidence would settle an indeterminate verdict, the second engine's verdict, and the certificate. [Verdicts and Proofs](#verdicts-and-proofs) explains how to read it.

## Reports

Verdicts over time from the database: pass rate by day, by target, by protocol, by scenario and by person, suite history, load history, and a CSV of every verdict in the period. See [Reports and the Activity Log](#reports-and-the-activity-log).

## Users and Activity

For administrators: the accounts, their roles and how each signs in; and the activity log of who signed in, who changed an account and who ran what. See [Accounts and Signing In](#accounts-and-signing-in).

## What a viewer sees

A viewer can open every screen and every proof, and use the screens that only compute — Validate, Transform, Card security and Counterparty — but the controls that run, store or send something — Run, Replay, Add account — are not offered. The server refuses those requests from a viewer regardless; hiding the controls only saves pressing a button to be told no.
