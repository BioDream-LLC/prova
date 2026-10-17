# Scenarios and Scripting

## What a scenario is

A scenario drives one message and asks for a verdict. It has four parts:

- a **request**: the message to send, as fields (`Amount`, `Currency`, `DebtorAcct`, `EndToEndId` and so on);
- a **context**: what you have seeded at the target and therefore know — the available balance, the currencies the account holds, an instruction id already processed;
- a **protocol**: `iso20022`, `iso8583` or `fix`, and optionally the **message** to send within it (`pacs.009`, `0400`, `F` …; each protocol has a default);
- an **expectation**: a sentence for the person reading the result.

The expectation is for people. The verdict does not come from it: prova never compares the response with a hand-written expected value when a proof is available. It decides the verdict from the casebook, the context and the response, which is why a scenario cannot pass because its author wrote down the wrong answer.

> The context is the part that makes a verdict possible. A scenario that sends €150.00 without saying what the account holds can only ever be indeterminate for the funds rule — prova will say so, and say that the balance would settle it.

## Running a scenario

On the **Scenarios** screen choose a scenario — filter by protocol or search — and a target of the same protocol, and press **Run**. From the command line:

```
prova run --scenario pacs008-over-balance --target cbpr-uat
```

prova ships 66 scenarios: 33 for ISO 20022, 17 for ISO 8583 and 16 for FIX. Every one is listed in the [Scenario Reference](#scenario-reference); each passes against the compliant simulated target of its protocol, and every one the compliant target refuses fails (or is indeterminate) against the defective one.

## Scripting

Scenarios are written in **JavaScript** or **Lua**. A script runs in a sandbox: no files, no network, no processes, no module loading, and a hard time limit. The whole language is one call:

```
scenario("payment over balance", {
  protocol: "iso20022",
  request:  { Amount: "150.00", Currency: "EUR", DebtorAcct: "ACC-EUR-1", EndToEndId: "E2E-1" },
  context:  { availableBalance: "142.00", expectReason: "AM04" },
  expect:   "reject (AM04)",
})
```

Add `message` to send something other than the protocol's default:

```
scenario("cancel a pending payment", {
  protocol: "iso20022",
  message:  "camt.056",
  request:  { OrgnlEndToEndId: "E2E-ORIG-PENDING", EndToEndId: "CXL-1", Reason: "DUPL" },
  context:  { originalPending: "true" },
  expect:   "cancel (CNCL)",
})
```

Because it is the real language, a loop gives you data-driven testing:

```
for (const amount of ["141.99", "142.00", "142.01"]) {
  scenario("boundary " + amount, {
    protocol: "iso20022",
    request:  { Amount: amount, Currency: "EUR", DebtorAcct: "ACC-EUR-1", EndToEndId: gen.e2e() },
    context:  { availableBalance: "142.00" },
    expect:   "accept up to the balance, reject above it",
  })
}
```

## Synthetic data

The `gen` object produces realistic, standards-valid values inside a script:

| Call | Produces |
|---|---|
| `gen.pan("4")` | a Luhn-valid card number with the given prefix |
| `gen.iban("DE")` | an IBAN with a correct mod-97 check |
| `gen.bic("DE")` | a BIC for the country |
| `gen.account()` | an account identifier |
| `gen.e2e()` | a unique end-to-end id |
| `gen.stan()` | an ISO 8583 system trace audit number |
| `gen.amount(min, max)` | a decimal amount in the range |

Call `seed(n)` first and the same seed yields the same stream of values every time, so a data-driven run that found a defect can be run again exactly.

## Context keys

The casebook reads these context keys. A rule whose key is absent stays dormant rather than guessing.

| Key | Meaning | Rule |
|---|---|---|
| `availableBalance` | funds available to the debtor, in major units | funds sufficiency |
| `debit` | `false` for an instruction that does not debit the account (a return, a reversal): the funds rule then stays out of it | funds sufficiency |
| `overdraftLimit` | `unknown` when an overdraft might authorise the payment: an acceptance is then indeterminate, not a failure | funds sufficiency |
| `expectReason` | the reason code a correct rejection carries (`AM04`, `51`, `3` …) | expected reason |
| `accountCurrency` | the currency the account holds | currency consistency |
| `fxAllowed` | `true` when a currency conversion is agreed | currency consistency |
| `valueDate`, `settlementDate` | the instruction's value date and the date it settles | value date |
| `backValueAllowed` | `true` when back-valuing is permitted | value date |
| `duplicate` | `true` when the target has already processed this instruction (or order, or STAN) | idempotency |
| `accountStatus` | `closed`, `blocked`, `frozen`, `lost` or `stolen` | account usable |
| `processingDate` | the date a card's expiry (`Expiry`, YYMM, in the request) is checked against | account usable |
| `transactionLimit` | the per-transaction limit, in major units | transaction limit |
| `originalKnown` | `false` when the original a return, reversal, cancel or status request names was never processed | original known |
| `originalAmount` | the amount of that original | within original |
| `originalFinal` | `true` when the original is settled, returned or filled and can no longer be cancelled | cancellation |
| `originalPending` | `true` when the original is still pending and a cancellation must succeed | cancellation |
| `originalStatus` | the status a status report must state (`ACSC`, `PDNG`; FIX `0`, `2` …) | status report |
| `marketOpen` | `false` while the market is closed or the instrument halted | market open |
| `instrumentKnown` | `false` for an instrument the venue does not list | instrument listed |
| `position`, `shortSellAllowed` | the quantity held, and `true` when short selling is permitted | position |

Field validation needs no context: prova checks the request itself — the currency is an ISO 4217 code, the amount is positive with no more decimals than the currency allows, BICs are well formed, IBANs and card numbers pass their check digits, and order quantities and prices are positive.
