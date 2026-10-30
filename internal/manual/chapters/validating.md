# Validating messages

Driving a target proves how it *behaves*. Validating a message checks whether the message itself is
well formed — before it ever reaches a counterparty. The two are different questions, and prova
answers both.

## Schema, profile and guideline

The **Validate** screen (and `prova validate` on the command line) checks an ISO 20022 message in three
layers:

- **The XML schema.** prova carries the official ISO 20022 schemas for the messages it speaks —
  pacs.002, pacs.003, pacs.004, pacs.008, pacs.009, pacs.010, pacs.028, camt.029, camt.052, camt.053,
  camt.054, camt.056, camt.060, pain.001, pain.002 and admi.002 — and validates the message against the
  one its namespace names: every mandatory element, the element order, the code lists, the amount and
  date formats, the BIC, IBAN and UETR patterns. prova's schema validator agrees with libxml2 on all
  363 messages of its test corpus: real messages, everything prova itself sends and replies, and
  deliberately broken mutations of them.
- **The usage-guideline profile.** A message can be schema-valid and still be refused by a clearing
  system. Choose a profile and prova applies its rules too:

| Profile | Messages | Rules |
|---|---|---|
| **CBPR+** | pacs.008, pacs.009, pain.001 | structured or hybrid postal addresses (fully unstructured refused from November 2026), a UUIDv4 UETR, valid LEIs and BICs |
| **SEPA** | pacs.008, pain.001 | euro only; 0.01 to 999,999,999.99; service level `SEPA`; charges `SLEV`; IBANs; names of at most 70 characters; one unstructured remittance line of at most 140; the EPC Latin character set; identifiers that neither start nor end with `/` nor contain `//`; structured or hybrid addresses |
| **SEPA-Inst** | pacs.008, pain.001 | every SEPA rule, and local instrument `INST` |
| **FedNow** | pacs.008, pacs.004 | US dollars; at most the $1,000,000 network limit; one transaction per message; charges `SLEV`; agents identified by a USABA routing number whose ABA check digit is right; debtor and creditor names; a creation time not in the future |

  The SEPA rules follow the European Payments Council's public implementation guidelines and the FedNow
  rules the FedNow Service operating procedures. Each scheme has more rules than these, and changes
  them: a profile catches what it names, and its description on the screen says what that is.

- **Your usage-guideline schema.** A market infrastructure publishes its usage guideline as a
  restricted schema, and SWIFT's MyStandards exports one for every guideline it holds. Give prova that
  XSD (**guideline XSD** on the screen, `--schema` on the command line) and it validates the message
  against it as well as against the base schema. Its findings carry rule ids `guideline.*`, and a
  finding the base schema already makes is not repeated. A guideline for another message is refused.

```
prova validate --profile SEPA payment.xml
prova validate --schema my-guideline.xsd --sarif payment.xml
```

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

> What prova validates is bounded and stated plainly: it reads usage-guideline schemas but is not
> SWIFT's MyStandards, and a clean result is not a guarantee a scheme will accept your message. It is
> an independent check you can run yourself, that names its rules and shows its working.
