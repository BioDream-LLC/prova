# Validating messages

Driving a target proves how it *behaves*. Validating a message checks whether the message itself is
well formed — before it ever reaches a counterparty. The two are different questions, and prova
answers both.

## Schema and profile

The **Validate** screen (and `prova validate` on the command line) checks an ISO 20022 message in two
layers:

- **The XML schema.** prova carries the official ISO 20022 schemas for the messages it speaks —
  pacs.008/009, pacs.002, pacs.004, pacs.028, camt.056/029/053/054 and pain.001/002 — and validates
  the message against the one its namespace names: every mandatory element, the element order, the
  code lists, the amount and date formats, the BIC, IBAN and UETR patterns. prova's schema validator
  is checked against libxml2 on a corpus of real messages and deliberately broken mutations; the two
  agree on every one.
- **The usage-guideline profile.** A message can be schema-valid and still be refused by a clearing
  system. With the **CBPR+** profile selected, prova also applies the cross-border rules that bite
  hardest in the SR2025 migration: a structured (not fully free-text) postal address, a valid
  end-to-end UETR, and well-formed LEIs and BICs.

## Reading the findings

Every finding names three things: the **rule** that fired (`xsd.minOccurs`, `cbpr.address.structured`
…), the **path** to the element it points at, and — wherever prova can give one — the **fix**. A
finding you cannot act on is one you have to ask somebody about, so prova tries never to stop at
"invalid".

A clean message reports no error-severity findings. A missing UETR under CBPR+ is a warning rather
than an error, because the schema permits it but the profile expects it; the report says which is
which.

## SARIF and CI

The findings export as **SARIF 2.1.0**, the format GitHub code scanning and editors read. From the
command line:

```
prova validate --profile CBPR+ --sarif payment.xml
```

`prova validate` exits non-zero when the message has an error-severity finding, so it gates a build:
a malformed message fails in CI rather than at a correspondent.

## Acting as the counterparty

The **Counterparty** screen is the mirror image: instead of validating a message you were sent, you
compose the reply you would send. Choose a protocol and a decision — accept, or reject with a reason
code — and prova renders the exact wire message a compliant host, switch or venue would return, and
the normalized fields it decides on. It is the same encoder the built-in simulators use, driven by
hand.

> What prova validates is bounded and stated plainly: it is not SWIFT's MyStandards, and a clean
> result is not a guarantee a scheme will accept your message. It is an independent check you can run
> yourself, that names its rules and shows its working.
