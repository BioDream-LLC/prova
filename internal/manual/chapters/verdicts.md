# Verdicts and Proofs

## Pass, fail and indeterminate

Every run ends in one of three verdicts.

- **Pass** — the target did what every applicable rule requires.
- **Fail** — a rule was violated, and the proof names the rule and the evidence.
- **Indeterminate** — the evidence does not decide it, and prova says what would.

Indeterminate is not a polite failure. If a host accepts a payment larger than the balance and the scenario says an overdraft might cover it, the acceptance may be right or wrong, and the honest answer is that the overdraft limit would settle it. prova lists that under **would settle it** on the Proof screen rather than guessing in either direction. A suite run on the command line treats indeterminate as a failure unless `--allow-indeterminate` is given.

## The casebook

The verdict is decided by the **casebook**: a set of behavioural rules, each enforcing one control. The rules read the canonical decision keys, so each one judges every protocol.

| Rule | A correct target |
|---|---|
| structural validity | answers with a recognised status, and gives a reason with every rejection |
| funds sufficiency | rejects a payment that exceeds available funds (`AM04`, DE39 `51`, a FIX reject) |
| currency consistency | rejects an instruction in a currency the account does not hold, unless conversion is agreed (`AM03`) |
| value date | rejects a back-valued instruction (`DT01`) |
| idempotency | rejects a duplicate instruction rather than processing it twice (`AM05`) |

A rule stays dormant unless the scenario's context supplies what it needs, and the rules compose: a rejection for one valid reason is not held against another rule.

## Reading a proof

A proof is an **argumentation graph**. Each node is a claim; each edge is an attack of one of three kinds:

- **rebut** — two conclusions conflict;
- **undermine** — an argument shows a premise is false;
- **undercut** — an argument shows an inference does not apply.

Under grounded semantics a claim is **in** when every claim attacking it is out, **out** when some claim attacking it is in, and **undecided** otherwise. The sink of every graph is the claim *the host behaved as the rule requires*. If it is in, the verdict is pass; if a violation defeats it, fail; if it is undecided, indeterminate.

Below the graph, the **proof chain** lists each step — the claim, why it holds, and the evidence it cites (the amounts compared, the response code received). That chain is what goes into a CSV export or an audit file.

## The second engine

Every case is decided twice. Besides the casebook, prova runs a second, independent engine: the same controls written as a rulebook in **quorum**'s casebook language and compiled by quorum's `cbc` compiler. Before `cbc` emits anything it solves every combination of readings — 4,096 states — and refuses a rulebook in which any state selects two verdicts or none.

At run time prova turns the exchange into yes/no readings (the host accepted; the amount is over the balance; the currency differs …), doing all the arithmetic and date comparison itself, and the compiled framework decides. The rules were written twice, in two languages, by two constructions, so agreement is corroboration. The Proof screen shows the second engine's verdict, the readings it saw and the fingerprint of the compiled rulebook. A disagreement is shown in red, printed by `prova run`, and counted on the Reports screen.

## The certificate

Every verdict carries a **certificate** from an independent re-check of its graph.

| Field | Meaning |
|---|---|
| engine | who checked it: `prova-proof+cadical`, `prova-proof` or `builtin-go` |
| query label | the grounded label of the verdict claim |
| complete | the labelling is a complete extension |
| grounded re-proved | the grounded label was derived again by complete-extension semantics |
| stable exists | the graph has a stable extension |
| skeptical / credulous | the claim is in every / some stable extension |

When the **prova-proof** companion is attached, the check is done in C++ with a SAT solver; built against CaDiCaL it signs as `prova-proof+cadical`. Without the companion prova performs the same checks in-process and signs as `builtin-go`. The engine is shown on the Proof screen and recorded with the run.
