# The Web Interface

Everything prova does can be done from the web interface. A feature that can only be reached from a terminal is treated as a defect. The interface is served by the binary itself and needs nothing else.

## The sidebar

The left rail lists every screen. At its foot is your account: your name, your role and how you signed in, and the sign-out button. The **Users** and **Activity** screens appear only for administrators. **Manual** opens this manual in a new tab, served by the same server, so it always matches the version you are running.

## Dashboard

The dashboard is the state of testing at a glance: how many verdicts passed, failed and were indeterminate, the latency of the targets, and the most recent runs. Click any run to open its proof.

## Scenarios

The scenario library and the script editor. Filter the library by protocol or search it, and choose a scenario to see the message it sends, its request, the context it seeds, and what it expects; choose a target — only those that speak the scenario's protocol are offered — and run it. The editor takes JavaScript or Lua (see [Scenarios and Scripting](#scenarios-and-scripting)) and runs what you write against the chosen target.

## Suites

The shipped regression and certification packs, each with a default target that can be changed before running. A suite's result is an aggregate verdict and a row per scenario, each linking to its proof. The **Load** panel on the same screen runs one scenario many times at a chosen concurrency.

## Messages

The wire exchange of a run, message by message: the fields as prova decoded them and the raw bytes as they crossed the connection. For FIX this includes the session messages; for ISO 8583 the bitmap and data elements.

## Transform

MT103 in, pacs.008 out, and back. Paste a message, convert it, and see every mapped field. See [Protocols](#protocols).

## Proof

The heart of prova. For the selected run it shows the verdict, the argumentation graph that decided it, the cited proof chain, what evidence would settle an indeterminate verdict, the second engine's verdict, and the certificate. [Verdicts and Proofs](#verdicts-and-proofs) explains how to read it.

## Reports

Verdicts over time from the database: pass rate by day, by target, by protocol, by scenario and by person, suite history, load history, and a CSV of every verdict in the period. See [Reports and the Activity Log](#reports-and-the-activity-log).

## Users and Activity

For administrators: the accounts, their roles and how each signs in; and the activity log of who signed in, who changed an account and who ran what. See [Accounts and Signing In](#accounts-and-signing-in).

## What a viewer sees

A viewer can open every screen and every proof, but the controls that change something — Run, Convert, Add account — are not offered. The server refuses those requests from a viewer regardless; hiding the controls only saves pressing a button to be told no.
