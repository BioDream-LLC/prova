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
| field validity | rejects an instruction that fails field validation: an unknown currency, an amount that is not positive or has too many decimals, a malformed BIC, an IBAN or card number that fails its check digits, a non-positive quantity or price, a malformed PIN block, MAC or EMV chip data on a card transaction (`AM11`, `AM01`, `AM12`, `RC01`, `AC01`, DE39 `14`, `13`, `30`, FIX `13`) |
| funds sufficiency | rejects a payment that exceeds available funds (`AM04`, DE39 `51`, a FIX reject), and does not refuse one the account can cover |
| currency consistency | rejects an instruction in a currency the account does not hold, unless conversion is agreed (`AM03`) |
| value date | rejects a back-valued instruction (`DT01`) |
| idempotency | rejects a duplicate instruction, transmission or order rather than processing it twice (`AM05`, DE39 `94`, FIX `6`) |
| account usable | accepts nothing on a closed, blocked or frozen account or a lost, stolen or expired card (`AC04`, `AC06`, DE39 `41`, `43`, `54`) |
| transaction limit | rejects an amount over the per-transaction limit, whatever the balance (`AM02`, DE39 `61`) |
| original known | rejects a return, reversal, cancellation or status request for an original it never processed (`NOOR`, DE39 `25`, FIX `1`, `5`) |
| within original | does not return or reverse more than the original moved (`AM09`, DE39 `13`) |
| cancellation | refuses to cancel an original that is already final, and cancels one that is still pending (camt.029 `RJCR`/`CNCL`, FIX `102=0`) |
| status report | states the original's known status, not another (pacs.002 `ACSC`/`PDNG`, FIX `39`) |
| market open | accepts no order while the market is closed (FIX `2`) |
| instrument listed | accepts no order for an instrument it does not list (FIX `1`) |
| position | does not sell more than the position held without a short-selling facility (FIX `3`) |
| expected reason | when a control requires a rejection and the scenario names the code, rejects with that code |

A rule stays dormant unless the scenario's context supplies what it needs, and the rules compose: a rejection for one valid reason is not held against another rule.

## Reading a proof

A proof is an **argumentation graph**. Each node is a claim; each edge is an attack of one of three kinds:

- **rebut** — two conclusions conflict;
- **undermine** — an argument shows a premise is false;
- **undercut** — an argument shows an inference does not apply.

Under grounded semantics a claim is **in** when every claim attacking it is out, **out** when some claim attacking it is in, and **undecided** otherwise. The sink of every graph is the claim *the host behaved as the rule requires*. If it is in, the verdict is pass; if a violation defeats it, fail; if it is undecided, indeterminate.

Below the graph, the **proof chain** lists each step — the claim, why it holds, and the evidence it cites (the amounts compared, the response code received). That chain is what goes into a CSV export or an audit file.

## The second engine

Every case is decided twice. Besides the casebook, prova runs a second, independent engine: the same controls written as a rulebook in **quorum**'s casebook language and compiled by quorum's `cbc` compiler. Before `cbc` emits anything it solves every combination of its 23 readings — 8,388,608 states — and refuses a rulebook in which any state selects two verdicts or none.

At run time prova turns the exchange into yes/no readings (the host accepted; the amount is over the balance; the currency differs …), doing all the arithmetic and date comparison itself, and the compiled framework decides. The rules were written twice, in two languages, by two constructions — even the field validation is implemented twice — so agreement is corroboration. prova's test suite compares the two engines on 114,048 generated cases and on every shipped scenario against every simulated target; they agree on all of them. The Proof screen shows the second engine's verdict, the readings it saw and the fingerprint of the compiled rulebook. A disagreement is shown in red, printed by `prova run`, and counted on the Reports screen.

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
