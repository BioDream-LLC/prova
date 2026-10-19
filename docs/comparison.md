# Competitor capabilities & prova's response

Research of the finance protocol-testing market (jPOS / jPOS Client Simulator, Paragon
Web FASTest, Greenline VeriFIX, FIXSIM, X9Ware ISO20022 Desktop, Trace Financial, AskISO).
Content below is paraphrased from public vendor material (licensing compliance).

Each capability is itemised, then mapped to what prova ships. prova's differentiator throughout
is that a verdict is a **machine-checked proof** (grounded + SAT-backed stable semantics, cited),
not a bare pass/fail or an AI explanation.

| # | Competitor capability | Who | prova response |
|---|-----------------------|-----|----------------|
| 1 | Counterparty / host simulators (test with no live counterparty) | jPOS, Paragon, FIXSIM | **Built-in simulators** per protocol (ISO 20022 payment host, ISO 8583 switch, FIX venue), switchable compliant/buggy modes |
| 2 | Multi-protocol coverage (ISO 8583, FIX, ISO 20022, MT/MX) | all | **ISO 20022 + ISO 8583 + FIX 4.4** adapters behind one SPI |
| 3 | Field-level validation & inspection | VeriFIX, X9Ware | **Structural validation** casebook rule + the Messages field inspector |
| 4 | Functional + regression **suites** | Paragon, FIXSIM | **Suites**: group scenarios, run as a batch, aggregate verdict |
| 5 | Certification test packs (scheme test cases) | Paragon, VeriFIX | Curated **certification suite** of named cases |
| 6 | Performance / load testing, replay at scale | Paragon, VeriFIX | **Load runs**: N iterations at a concurrency, latency percentiles + throughput |
| 7 | Negative testing | Paragon | Negative scenarios + rules that prove the *rejection* is correct |
| 8 | CI/CD / headless automation | Paragon (DevSecOps), FIXSIM | **`prova run` CLI**: run a suite headless, JUnit + JSON report, non-zero exit on failure |
| 9 | Synthetic / realistic test data | X9Ware, AskISO | **Synthetic data generator** (Luhn PANs, IBAN/BIC, amounts, ids, timestamps) + data-driven scenarios |
| 10 | Reporting / auditable evidence | jPOS, Paragon | Every verdict is a cited proof + certificate; **export** JSON/JUnit |
| 11 | MT↔MX / transformation testing | Trace, AskISO | **MT103 ↔ pacs.008 transformation** (Transform screen + `/api/transform`), then drive and prove the migrated message |
| 12 | FIX session testing (logon/seqnum/heartbeat) | VeriFIX, FIXSIM | **FIX session layer**: Logon handshake, heartbeat/TestRequest, sequence numbers, BodyLength + CheckSum verification |
| 13 | Traffic capture & replay | Paragon, VeriFIX | **Record & replay**: capture a JSONL cassette, re-prove it offline with no live target |

prova has a response to each of the 13. That is coverage by category, not depth: the established tools
ship far more messages, dialects and cases. What prova has, and what is still missing:

| Area | prova | Gap |
|---|---|---|
| ISO 20022 messages | pacs.008/009, pacs.004, camt.056→camt.029, pacs.028→pacs.002, pain.001→pain.002 | camt.052/053/054 statements, pacs.003/010, admi; more message sets |
| ISO 20022 validation | full XSD validation (cross-checked vs libxml2) + CBPR+ structured-address, UETR and LEI rules, SARIF output | SEPA and FedNow profiles, MyStandards usage guidelines |
| ISO 8583 | 0100, 0200, 0400, 0800; secondary bitmap; DE39 codes; chip/PIN/MAC format checks (DE52/55/64, 9F26 ARQC) | scheme dialects (Visa Base I, Mastercard), cryptographic PIN/MAC verification, full EMV kernel, HSM |
| FIX | 4.4 and 5.0 SP2 (FIXT.1.1): D, F, G, H; ExecutionReport incl. partial fills, OrderCancelReject; session layer with ResendRequest/SequenceReset gap-fill | FIX 4.2, richer recovery edge cases, market data |
| Scenarios | 75 scenarios, 13 suites | case libraries of hundreds to thousands; official scheme certification sets |
| Rules | 16 controls, written twice and cross-checked | usage-guideline rules per market |

Beyond the categories, prova decides every verdict with **two independent engines** — its Go casebook
and quorum's cbc-compiled casebook (23 readings; cbc validates all 8,388,608 states), which agree on all
114,048 generated cases and on every shipped scenario — and re-checks the framework with a SAT solver
(CaDiCaL).
