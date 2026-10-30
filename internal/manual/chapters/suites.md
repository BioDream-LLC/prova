# Suites

## Running a suite

A suite runs a set of scenarios as one batch against a target and aggregates the result. The aggregate is **fail** if any scenario failed, **indeterminate** if any was indeterminate and none failed, and **pass** otherwise. Each scenario's run is stored with its proof and linked to the suite execution, so a failed certification run can be reopened months later exactly as it was.

prova ships 27 packs, listed in the [Suite Reference](#suite-reference). Run one from the **Suites** screen, choosing another target if needed, or from CI:

```
prova run --suite regression-iso20022 --junit results.xml
```

## The packs

| Area | Packs |
|---|---|
| ISO 20022 | regression; controls; field validation; identifiers and amounts (twenty countries' IBANs, BICs, every currency exponent, the one-cent boundary); returns, cancellations and status; payment initiation; direct debits; account reporting |
| ISO 8583 | regression; card controls; card numbers, expiry and boundaries; card security (formats); card cryptography (PIN, MAC, cryptogram verification); reversals and network |
| FIX | regression; order controls; order fields; order lifecycle; mass cancel; session rules; FIX 4.2 regression; FIX 5.0 SP2 regression |
| Certification-style | CBPR+ readiness; SEPA readiness (against `sepa-host`); FedNow readiness (against `fednow-host`); a Visa-style and a Mastercard-style card pack (against `visa-switch` and `mc-switch`) |

The certification-style packs are prova's own, built from the controls in this manual and from the schemes' public rules. They are not the schemes' certification test sets, which the schemes license to their members; a pass is evidence you can show, not a certificate a scheme issued.

A suite runs every scenario against its own target, whatever target a scenario was written for, so the same card controls run in the generic, Visa-style and Mastercard-style dialects. Load runs and the replay of captured traffic are in [Load and replay](#load-and-replay).
