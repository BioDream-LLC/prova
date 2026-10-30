# Protocols

prova speaks three wire protocols natively. Each adapter encodes the request, carries it over a real connection, decodes the response, and normalises the outcome to the same small set of decision keys — `decision` (`accepted` or `rejected`) and, for a rejection, `rejectReason` — so that one casebook judges an ISO 20022 `RJCT`, an ISO 8583 decline and a FIX reject alike.

## ISO 20022

prova sends these ISO 20022 messages over a length-prefixed session, set by the scenario's `message`:

| Message | Purpose | Reply |
|---|---|---|
| **pacs.008** (default) | FI-to-FI customer credit transfer | pacs.002 |
| **pacs.009** | financial institution credit transfer | pacs.002 |
| **pacs.003** | FI-to-FI customer direct debit, under the debtor's mandate | pacs.002 |
| **pacs.010** | financial institution direct debit | pacs.002 |
| **pacs.004** | payment return | pacs.002 |
| **camt.056** | FI-to-FI payment cancellation request | camt.029 resolution of investigation |
| **pacs.028** | FI-to-FI payment status request | pacs.002 stating the original's status |
| **pain.001** | customer credit transfer initiation | pain.002 customer payment status report |
| **camt.060** | account reporting request | camt.052 intraday report, camt.053 statement or camt.054 notification; admi.002 message reject when refused |

Every message prova writes is valid against the official schema of its version, and so is every reply its simulated host gives: the test suite validates each one with prova's validator, and the same messages sit in the corpus checked against libxml2. A test tool that sent malformed messages would blame the host for its own mistakes.

| Request field | Carries |
|---|---|
| `MsgId` | the message identification |
| `EndToEndId` | the end-to-end identification (the return, cancellation or status request id for those messages) |
| `UETR` | the unique end-to-end transaction reference; prova generates a version-4 UUID when none is given |
| `Amount`, `Currency` | the settlement, returned or original amount and its currency |
| `ValueDate` | the interbank settlement date |
| `DebtorAcct`, `CreditAcct` | the debtor and creditor accounts, as proprietary identifiers |
| `DebtorIBAN`, `CreditIBAN` | the accounts as IBANs, when given |
| `DebtorBIC`, `CreditBIC` | the debtor and creditor agents (assigner and assignee for camt.056) |
| `DebtorRTN`, `CreditRTN` | US agents by ABA routing number, as a USABA clearing-system member id (FedNow, Fedwire) |
| `DebtorName`, `CreditorName` | the parties' names; their postal address is structured (town and country) |
| `ServiceLevel`, `LocalInstrument` | the payment type, such as `SEPA` and `INST` |
| `ChargeBearer` | `SLEV` (the default), `SHAR`, `DEBT` or `CRED` |
| `Remittance` | one line of unstructured remittance information |
| `OrgnlEndToEndId` | the original a pacs.004, camt.056 or pacs.028 refers to |
| `Reason` | the return or cancellation reason code |
| `MandateId`, `MandateDate`, `SequenceType`, `CreditorSchemeId` | a direct debit's mandate, its signature date, FRST/RCUR/OOFF/FNAL and the creditor identifier |
| `Account`, `ReportKind`, `FromDate`, `ToDate` | a camt.060's account, the report it asks for (camt.052, camt.053 or camt.054) and its period |

A pacs.002 or pain.002 status of `RJCT`, a camt.029 of `RJCR`, or an admi.002 message reject is a canonical `rejected`, and its reason code (`AM04`, `AM03`, `AC04`, `MD01`, `RC01`, `NOOR` …) becomes `rejectReason`. Any other status is `accepted`, and is also recorded as `reportedStatus` — which is what the status-report rule compares with the original's known status. An account report is `accepted` and records the account, its closing balance and the number of entries it lists as `reportedAccount`, `reportedBalance` and `entries`, which the report rule compares with what the tester seeded.

> Format validation and behaviour are different questions. With the CBPR+ coexistence period ending in November 2026, every bank is checking that its messages are well-formed. A well-formed acceptance of a payment the account cannot cover is still a defect, and that is the question prova answers.

## ISO 8583

prova packs and unpacks ISO 8583 with a real bitmap packager, including the secondary bitmap for data elements above 64. The scenario's `message` is the request MTI:

| MTI | Purpose | Response |
|---|---|---|
| **0100** (default) | authorisation | 0110 |
| **0200** | financial transaction (single-message purchase) | 0210 |
| **0400** | reversal | 0410 |
| **0800** | network management (sign-on, sign-off, echo) | 0810 |

| Request field | Data element |
|---|---|
| `PAN` | DE2 primary account number |
| `ProcessingCode` | DE3 |
| `Amount` | DE4, in minor units |
| `STAN` | DE11 system trace audit number |
| `Expiry` | DE14 expiration date, YYMM |
| `POSEntryMode` | DE22 how the card was read (051 chip, 071 contactless chip, 010 manual) |
| `Terminal` | DE41 card acceptor terminal |
| `Currency` | DE49, as its ISO 4217 numeric code |
| `PINBlock` | DE52 ISO 9564 PIN block |
| `ICCData` | DE55 EMV chip data, BER-TLV |
| `MAC` | DE64 message authentication code |
| `NetworkCode` | DE70 network management code (`001`, `002`, `301`) |
| `OriginalSTAN` | the original's STAN, carried in DE90 original data elements |

DE39 `00` is `accepted`; any other response code is `rejected`, with the code (`51` insufficient funds, `14` invalid card number, `54` expired card, `55` incorrect PIN, `25` original not found …) as `rejectReason`. The card-security checks — PIN blocks, MACs and chip cryptograms — have their own chapter, [Card security](#card-security).

### Dialects

The data elements mean the same on every network, but each puts them on the wire differently. A target's `dialect` parameter chooses the encoding:

| Dialect | Numbers and MTI | Text | Lengths | Bitmaps | PIN block, MAC, chip data | Header |
|---|---|---|---|---|---|---|
| **generic** (default) | ASCII digits | ASCII | ASCII digits | hex text | hex text | none |
| **visa** | packed BCD | EBCDIC (code page 037) | one-byte binary (two for LLL) | binary | binary | the 22-byte BASE I-style header |
| **mastercard** | EBCDIC digits | EBCDIC | EBCDIC digits | binary | binary | none |

The Visa and Mastercard dialects follow public descriptions of those networks' encodings, and prova's packer agrees byte for byte with an independent codec (pyiso8583) on fifty messages in each. The schemes' own specifications are confidential and change with each release: certify on the schemes' test systems. In the binary dialects the PIN block and MAC are 8 bytes, so a format 4 (AES) PIN block, which is 16, travels only in the generic dialect.

## FIX

prova speaks FIX with a real `tag=value` codec, including the BodyLength (`9`) and CheckSum (`10`) computation, so a corrupt or truncated message is refused rather than misread. A target's `BeginString` parameter chooses the session version: **FIX.4.2**, **FIX.4.4** (the default) or **FIXT.1.1** carrying **FIX 5.0 SP2** (`DefaultApplVerID=9`). In FIX 4.2 a fill is ExecType `2` with ExecTransType `20=0`; in 4.4 and later it is ExecType `F`.

The scenario's `message` is the MsgType sent:

| MsgType | Message | Answered by |
|---|---|---|
| **D** (default) | NewOrderSingle | ExecutionReport: a fill, a partial fill or a rejection |
| **F** | OrderCancelRequest | ExecutionReport `150=4`, or OrderCancelReject (`35=9`) |
| **G** | OrderCancelReplaceRequest | ExecutionReport `150=5`, or OrderCancelReject |
| **H** | OrderStatusRequest | ExecutionReport `150=I` stating the order's status |
| **q** | OrderMassCancelRequest: every open order (`Symbol` `*`) or one instrument's | OrderMassCancelReport (`35=r`) with the number cancelled (`533`), or refused (`531=0`, `532`) |

Cancel, replace and status requests name their order in `OrigClOrdID`. An OrderCancelReject, a refused mass cancel, or an ExecutionReport with ExecType `150=8` or OrdStatus `39=8` is `rejected`, carrying its OrdRejReason (`103`), CxlRejReason (`102`) or MassCancelRejectReason (`532`) as `rejectReason`. A session Reject (`35=3`) is `rejected` with its SessionRejectReason (`373`); a Logout instead of an answer is `rejected` with `LOGOUT`.

### The session rules

prova runs the session layer — the Logon handshake, heartbeats, TestRequest, a sequence number per side (`34`) and recovery, where a ResendRequest is answered by a SequenceReset in gap-fill mode — and its simulated venue enforces the rules a counterparty relies on. The session scenarios each send an order with one deliberate fault, and the defective venue, which checks none of it, is caught by every one:

| Scenario message | The fault | What the venue must do |
|---|---|---|
| `session:seq-too-low` | a MsgSeqNum already used, without PossDupFlag | log out |
| `session:possdup-no-time` | PossDupFlag `43=Y` without OrigSendingTime (`122`) | Reject, `373=1` (required tag missing) |
| `session:possdup-replay` | an order it has filled, resent as a possible duplicate | ignore it, not fill it twice |
| `session:wrong-comp-id` | a TargetCompID other than the session's | Reject, `373=9` (CompID problem), and log out |
| `session:bad-checksum` | a wrong CheckSum | ignore it as garbled |

When silence is the right answer, prova waits a short window and records the outcome as `rejected` with reason `IGNORED`. A FIX 4.2 venue must reject an OrderMassCancelRequest with `373=11` (invalid MsgType): the message arrived in FIX 4.3.

## MT and MX

For the migration from SWIFT MT to ISO 20022, prova converts eight MT messages to their ISO 20022 counterparts and back, on the **Transform** screen or at `POST /api/transform`:

| MT | MX | What the pair carries |
|---|---|---|
| **MT103** | pacs.008 | references, value date, currency and amount (the decimal comma becomes a point), the ordering and beneficiary customers' accounts and names, the agents, field 71A as the charge bearer, field 70 as remittance |
| **MT202** | pacs.009 | the transaction and related references, the amount, the ordering and beneficiary institutions |
| **MT103 RETN** | pacs.004 | a return: field 72 `/RETN/`, its reason code and `/MREF/` (the original's reference) |
| **MT192** | camt.056 | a request for cancellation: the original's reference (`:21:`) and the `/CODE/` in field 79 |
| **MT196** | camt.029 | the answer: `/CNCL/` (cancelled) or a refusal with its reason |
| **MT900**, **MT910** | camt.054 | a confirmation of debit or credit, as one entry with its value date and reference |
| **MT940** | camt.053 | a statement: opening and closing balances (`:60F:`, `:62F:`) and each `:61:` line as an entry |

Every MX message prova produces is valid against its schema, and the MT940 it writes is read back by an independent parser (mt-940) in the test suite. Each transform lists what it could not carry across: MT reference fields hold 16 characters, so a longer ISO 20022 reference is cut, and some MT fields have no place in the MX message prova reads. The purpose is end to end — take a message a bank already sends, convert it, drive it at the host, and prove the host behaved correctly on the migrated form. It is not a complete SWIFT translation service.

## The simulated targets

prova starts its own targets so every feature works with nothing else running, seeded with the accounts, cards, mandates, originals and orders the shipped scenarios refer to:

| Target | Protocol | What it is |
|---|---|---|
| `cbpr-uat` | ISO 20022 | the compliant reference host |
| `switch-sim` | ISO 20022 | accepts everything (the defect to catch) |
| `sepa-host` | ISO 20022 | screens every message against the EPC SEPA rules before deciding it |
| `fednow-host` | ISO 20022 | a FedNow participant: US dollars, routing numbers, the network limit |
| `card-switch` | ISO 8583 | the compliant switch, generic dialect, with the published test keys |
| `card-switch-bug` | ISO 8583 | approves everything |
| `visa-switch`, `mc-switch` | ISO 8583 | compliant switches in the Visa-style and Mastercard-style dialects |
| `fix-venue` | FIX | the compliant venue (FIX 4.4) |
| `fix-venue-bug` | FIX | fills, cancels and replaces everything and ignores the session rules |
| `fix42-venue`, `fix50-venue` | FIX | compliant venues over FIX 4.2 and FIX 5.0 SP2 sessions |

They are listed in the [Target Reference](#target-reference). Start prova with `-no-sim` to run without them.
