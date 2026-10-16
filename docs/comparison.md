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

All 13 are implemented. Beyond parity, prova decides every verdict with **two independent engines** —
its Go casebook and quorum's cbc-compiled casebook, which agree on all 3,456 combinations in the
cross-engine test — and re-checks the framework with a SAT solver (CaDiCaL).
