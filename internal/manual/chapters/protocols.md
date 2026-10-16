# Protocols

prova speaks three wire protocols natively. Each adapter encodes the request, carries it over a real connection, decodes the response, and normalises the outcome to the same small set of decision keys — `decision` (`accepted` or `rejected`) and, for a rejection, `rejectReason` — so that one casebook judges an ISO 20022 `RJCT`, an ISO 8583 decline and a FIX reject alike.

## ISO 20022

prova sends **pacs.008** (FI-to-FI customer credit transfer) and reads the **pacs.002** payment status report in reply, over a length-prefixed session.

| Request field | Carries |
|---|---|
| `MsgId` | the message identification |
| `EndToEndId` | the end-to-end identification |
| `Amount`, `Currency` | the interbank settlement amount and its currency |
| `ValueDate` | the interbank settlement date |
| `DebtorAcct`, `CreditAcct` | the debtor and creditor accounts |
| `DebtorBIC`, `CreditBIC` | the debtor and creditor agents |

A transaction status of `ACSP` is a canonical `accepted`; any other status is `rejected`, and the status reason code (`AM04`, `AM03`, `AM05`, `DT01` …) becomes `rejectReason`.

> Format validation and behaviour are different questions. With the CBPR+ coexistence period ending in November 2026, every bank is checking that its messages are well-formed. A well-formed acceptance of a payment the account cannot cover is still a defect, and that is the question prova answers.

## ISO 8583

prova packs and unpacks ISO 8583 authorisation messages — MTI **0100** request, **0110** response — with a real ASCII bitmap packager.

| Request field | Data element |
|---|---|
| `PAN` | DE2 primary account number |
| `ProcessingCode` | DE3 |
| `Amount` | DE4, in minor units |
| `STAN` | DE11 system trace audit number |
| `Terminal` | DE41 card acceptor terminal |
| `Currency` | DE49, as its ISO 4217 numeric code |

DE39 `00` is `accepted`; any other response code is `rejected`, with the code (`51` insufficient funds, `05` do not honour …) as `rejectReason`.

## FIX

prova speaks **FIX 4.4** with a real `tag=value` codec, including the BodyLength (`9`) and CheckSum (`10`) computation, so a corrupt or truncated message is refused rather than misread.

It runs the session layer: the Logon handshake, heartbeats, TestRequest and Heartbeat, and a sequence number per side (`34`). The application messages are **NewOrderSingle** (`35=D`) and **ExecutionReport** (`35=8`).

A Reject (`35=3`), or an ExecutionReport with ExecType `150=8` or OrdStatus `39=8`, is `rejected`, carrying its OrdRejReason (`103`) as `rejectReason`. Any other ExecutionReport — a fill, `39=2` — is `accepted`.

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

prova starts its own targets so every feature works with nothing else running. Two have planted defects, so you can watch prova catch them. They are listed in the [Target Reference](#target-reference). Start prova with `-no-sim` to run without them.
