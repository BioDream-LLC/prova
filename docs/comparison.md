# Competitor capabilities & prova's response

Research of the finance protocol-testing market (jPOS / jPOS Client Simulator, Paragon
Web FASTest, Greenline VeriFIX, FIXSIM, X9Ware ISO20022 Desktop, Trace Financial, AskISO).
Content below is paraphrased from public vendor material (licensing compliance).

Each capability is itemised, then mapped to what prova ships. prova's differentiator throughout
is that a verdict is a **machine-checked proof** (grounded + SAT-backed stable semantics, cited),
not a bare pass/fail or an AI explanation.

| # | Competitor capability | Who | prova response |
|---|-----------------------|-----|----------------|
| 1 | Counterparty / host simulators (test with no live counterparty) | jPOS, Paragon, FIXSIM | **Twelve built-in simulators**: payment hosts (reference, SEPA, FedNow), card switches (generic, Visa-style, Mastercard-style), FIX 4.2/4.4/5.0 venues, and a defective one per protocol |
| 2 | Multi-protocol coverage (ISO 8583, FIX, ISO 20022, MT/MX) | all | **ISO 20022 + ISO 8583 (three dialects) + FIX 4.2 to 5.0 SP2** adapters behind one SPI |
| 3 | Field-level validation & inspection | VeriFIX, X9Ware | **Structural validation** casebook rule + the Messages field inspector |
| 4 | Functional + regression **suites** | Paragon, FIXSIM | **Suites**: group scenarios, run as a batch, aggregate verdict |
| 5 | Certification test packs (scheme test cases) | Paragon, VeriFIX | prova's own **CBPR+, SEPA, FedNow, Visa-style and Mastercard-style packs** |
| 6 | Performance / load testing, replay at scale | Paragon, VeriFIX | **Load runs** by count or duration, at a rate, over persistent sessions, with a timeline; **live replay of imported production traffic** with masking |
| 7 | Negative testing | Paragon | Negative scenarios + rules that prove the *rejection* is correct |
| 8 | CI/CD / headless automation | Paragon (DevSecOps), FIXSIM | **`prova run` CLI**: run a suite headless, JUnit + JSON report, non-zero exit on failure |
| 9 | Synthetic / realistic test data | X9Ware, AskISO | **Synthetic data generator** (Luhn PANs, IBAN/BIC, amounts, ids, timestamps) + data-driven scenarios |
| 10 | Reporting / auditable evidence | jPOS, Paragon | Every verdict is a cited proof + certificate; **export** JSON/JUnit |
| 11 | MT↔MX / transformation testing | Trace, AskISO | **Eight MT ↔ MX pairs** (MT103, MT202, MT103 RETN, MT192, MT196, MT900, MT910, MT940), every MX output schema-valid |
| 12 | FIX session testing (logon/seqnum/heartbeat) | VeriFIX, FIXSIM | **FIX session layer and session rules** (sequence too low, possible duplicates, comp ids, garbled messages, gap fill) on FIX 4.2, 4.4 and 5.0 SP2 |
| 13 | Traffic capture & replay | Paragon, VeriFIX | **Record & replay** offline, and **import** of FIX logs, ISO 8583 dumps and ISO 20022 files replayed live |

prova has a response to each of the 13. That is coverage by category, not depth: the established tools
ship far more messages, dialects and cases. What prova has, and what is still missing:

| Area | prova | Gap |
|---|---|---|
| ISO 20022 messages | pacs.008/009, pacs.003/010, pacs.004, camt.056→camt.029, pacs.028→pacs.002, pain.001→pain.002, camt.060→camt.052/053/054 or admi.002; all schema-valid | the rest of the catalogue (hundreds of messages) |
| ISO 20022 validation | official XSDs (libxml2-checked on 363 messages), CBPR+, SEPA, SEPA Inst and FedNow rules, your usage-guideline XSD, SARIF | every published usage guideline maintained for you |
| ISO 8583 | 0100, 0200, 0400, 0800 in generic, Visa-style and Mastercard-style dialects (pyiso8583-checked); PIN blocks 0/1/3/4, ISO 9797-1 MACs, AES-CMAC, Visa CVN 10/18 and Mastercard CVN 16 cryptograms verified; payShield HSM client | other networks' dialects; full EMV kernel (CDA, offline data authentication); more HSM command sets |
| FIX | 4.2, 4.4 and 5.0 SP2: D, F, G, H, q; fills, partial fills, cancel rejects, mass cancel reports; the session rules | FIX 4.0/4.1, market data, FIXP and SBE |
| Scenarios | 1,745 scenarios, 32 suites | case libraries of thousands; the schemes' official certification sets |
| Rules | 19 controls, written twice and cross-checked, including card cryptography | — |

Beyond the categories, prova decides every verdict with **two independent engines** — its Go casebook
and quorum's cbc-compiled casebook (26 readings; cbc validates all 67,108,864 states), which agree on all
120,204 generated cases and on every shipped scenario — and re-checks the framework with a SAT solver
(CaDiCaL).
