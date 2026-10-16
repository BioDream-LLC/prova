# Scenarios and Scripting

## What a scenario is

A scenario drives one message and asks for a verdict. It has four parts:

- a **request**: the message to send, as fields (`Amount`, `Currency`, `DebtorAcct`, `EndToEndId` and so on);
- a **context**: what you have seeded at the target and therefore know — the available balance, the currencies the account holds, an instruction id already processed;
- a **protocol**: `iso20022`, `iso8583` or `fix`;
- an **expectation**: a sentence for the person reading the result.

The expectation is for people. The verdict does not come from it: prova never compares the response with a hand-written expected value when a proof is available. It decides the verdict from the casebook, the context and the response, which is why a scenario cannot pass because its author wrote down the wrong answer.

> The context is the part that makes a verdict possible. A scenario that sends €150.00 without saying what the account holds can only ever be indeterminate for the funds rule — prova will say so, and say that the balance would settle it.

## Running a scenario

On the **Scenarios** screen choose a scenario and a target and press **Run**. From the command line:

```
prova run --scenario pacs008-over-balance --target cbpr-uat
```

Every shipped scenario is listed in the [Scenario Reference](#scenario-reference).

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
| `expectReason` | the reason code a correct rejection carries (`AM04`, `51`) | funds sufficiency |
| `overdraftLimit` | `unknown` when an overdraft might authorise the payment: an acceptance is then indeterminate, not a failure | funds sufficiency |
| `accountCurrency` | the currency the account holds | currency consistency |
| `fxAllowed` | `true` when a currency conversion is agreed | currency consistency |
| `valueDate`, `settlementDate` | the instruction's value date and the date it settles | value date |
| `backValueAllowed` | `true` when back-valuing is permitted | value date |
| `duplicate` | `true` when the target has already processed this instruction id | idempotency |
