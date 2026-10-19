# Protocols

prova speaks three wire protocols natively. Each adapter encodes the request, carries it over a real connection, decodes the response, and normalises the outcome to the same small set of decision keys — `decision` (`accepted` or `rejected`) and, for a rejection, `rejectReason` — so that one casebook judges an ISO 20022 `RJCT`, an ISO 8583 decline and a FIX reject alike.

## ISO 20022

prova sends five ISO 20022 messages over a length-prefixed session, set by the scenario's `message`:

| Message | Purpose | Reply |
|---|---|---|
| **pacs.008** (default) | FI-to-FI customer credit transfer | pacs.002 |
| **pacs.009** | financial institution credit transfer | pacs.002 |
| **pacs.004** | payment return | pacs.002 |
| **camt.056** | FI-to-FI payment cancellation request | camt.029 resolution of investigation |
| **pacs.028** | FI-to-FI payment status request | pacs.002 stating the original's status |
| **pain.001** | customer credit transfer initiation | pain.002 customer payment status report |

| Request field | Carries |
|---|---|
| `MsgId` | the message identification |
| `EndToEndId` | the end-to-end identification (the return, cancellation or status request id for those messages) |
| `Amount`, `Currency` | the settlement, returned or original amount and its currency |
| `ValueDate` | the interbank settlement date |
| `DebtorAcct`, `CreditAcct` | the debtor and creditor accounts, as proprietary identifiers |
| `DebtorIBAN`, `CreditIBAN` | the accounts as IBANs, when given |
| `DebtorBIC`, `CreditBIC` | the debtor and creditor agents (assigner and assignee for camt.056) |
| `OrgnlEndToEndId` | the original a pacs.004, camt.056 or pacs.028 refers to |
| `Reason` | the return or cancellation reason code |

A customer initiation (pain.001) is answered by a pain.002 with the same status semantics. A pacs.002 status of `RJCT`, or a camt.029 of `RJCR`, is a canonical `rejected`, and its reason code (`AM04`, `AM03`, `AC04`, `RC01`, `NOOR` …) becomes `rejectReason`. Any other status is `accepted`, and is also recorded as `reportedStatus` — which is what the status-report rule compares with the original's known status.

> Format validation and behaviour are different questions. With the CBPR+ coexistence period ending in November 2026, every bank is checking that its messages are well-formed. A well-formed acceptance of a payment the account cannot cover is still a defect, and that is the question prova answers.

## ISO 8583

prova packs and unpacks ISO 8583 with a real ASCII bitmap packager, including the secondary bitmap for data elements above 64. The scenario's `message` is the request MTI:

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
| `Terminal` | DE41 card acceptor terminal |
| `Currency` | DE49, as its ISO 4217 numeric code |
| `NetworkCode` | DE70 network management code (`001`, `002`, `301`) |
| `OriginalSTAN` | the original's STAN, carried in DE90 original data elements |
| `POSEntryMode` | DE22 how the card was read (051 chip, 071 contactless chip, 010 manual) |
| `PINBlock` | DE52 ISO 9564 PIN block |
| `ICCData` | DE55 EMV chip data, BER-TLV |
| `MAC` | DE64 message authentication code |

A chip-read authorisation must carry well-formed EMV data with an Application Cryptogram (9F26), a 16-hex PIN block and a 16- or 32-hex MAC; a malformed one is declined (DE39=30). DE39 `00` is `accepted`; any other response code is `rejected`, with the code (`51` insufficient funds, `14` invalid card number, `54` expired card, `25` original not found …) as `rejectReason`.

## FIX

prova speaks **FIX 4.4** with a real `tag=value` codec, including the BodyLength (`9`) and CheckSum (`10`) computation, so a corrupt or truncated message is refused rather than misread.

It speaks FIX 4.4 and, over a FIXT.1.1 session, FIX 5.0 SP2 (the application version travels in DefaultApplVerID/ApplVerID). It runs the session layer: the Logon handshake, heartbeats, TestRequest and Heartbeat, a sequence number per side (`34`), and recovery — a ResendRequest is answered by a SequenceReset in gap-fill mode. The scenario's `message` is the MsgType sent:

| MsgType | Message | Answered by |
|---|---|---|
| **D** (default) | NewOrderSingle | ExecutionReport: a fill or a rejection |
| **F** | OrderCancelRequest | ExecutionReport `150=4`, or OrderCancelReject (`35=9`) |
| **G** | OrderCancelReplaceRequest | ExecutionReport `150=5`, or OrderCancelReject |
| **H** | OrderStatusRequest | ExecutionReport `150=I` stating the order's status |

Cancel, replace and status requests name their order in `OrigClOrdID`. A Reject (`35=3`), an OrderCancelReject, or an ExecutionReport with ExecType `150=8` or OrdStatus `39=8` is `rejected`, carrying its OrdRejReason (`103`) or CxlRejReason (`102`) as `rejectReason`. Any other ExecutionReport is `accepted`; a partial fill (ExecType=1, OrdStatus=1) reports CumQty and LeavesQty, and a status report's `39` is recorded as `reportedStatus`.

## MT103 and pacs.008

For the migration from SWIFT MT to ISO 20022, prova converts an **MT103** (single customer credit transfer) to a **pacs.008** and back, on the **Transform** screen or at `POST /api/transform`.

| MT103 field | pacs.008 |
|---|---|
| `:20:` sender's reference | message and end-to-end identification |
| `:32A:` value date, currency, amount | settlement date, currency and amount — SWIFT's decimal comma becomes a point |
| `:50K:` ordering customer | debtor account |
| `:52A:` ordering institution | debtor agent |
| `:57A:` account with institution | creditor agent |
| `:59:` beneficiary customer | creditor account |

The purpose is end to end: take a message a bank already sends, convert it, drive it at the host, and prove the host behaved correctly on the migrated form. The converter covers the fields that matter for that; it is not a complete SWIFT parser, and fields outside this table are not carried across.

## The simulated targets

prova starts its own targets so every feature works with nothing else running: a compliant and a defective target for each protocol, seeded with the accounts, cards, originals and orders the shipped scenarios refer to. The defective ones accept everything, so you can watch prova catch them. They are listed in the [Target Reference](#target-reference). Start prova with `-no-sim` to run without them.
