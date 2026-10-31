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

## The ISO 20022 catalogue

prova carries every schema the ISO 20022 Registration Authority publishes: 2,641 versions of 707 messages in 31 business areas, from payments and cash management to securities, cards, trade services, FX and reference data. Any of them validates with no setup; the namespace of the message picks its schema, including the older `urn:swift:xsd:` and `urn:iso:std:iso:20022:xsd:` forms.

For any version prova also builds instances: a minimal sample with only what the schema requires, a full one with every optional element once, and broken variants — a required element removed, an element the schema does not define, a code outside its list, a value that breaks its pattern or length, an impossible date, a malformed number, a duplicate, a missing attribute, two elements swapped. The validator's verdict on all of these, 38,428 cases over the 2,641 schemas, agrees with libxml2's.

The **Catalogue** screen browses the areas and messages, shows any version's instance, checks it, and sends it to an ISO 20022 target, which answers as a network gateway does: an admi.007 receipt acknowledgement for a valid message, an admi.002 reject naming the first schema error otherwise. The catalogue suites send the latest version of every message both ways.

```
prova catalogue list sese                   # the latest version of every securities settlement message
prova catalogue sample camt.053 full        # a full statement, valid against camt.053.001.12
prova catalogue sample pacs.008 drop        # with a required element removed
prova catalogue schema seev.031.001.14      # the official XSD
```

## The usage-guideline library

A usage guideline is the restricted schema a market infrastructure publishes for the messages it accepts. prova bundles TCH's RTP guidelines (22 schemas, from moov-io/rtp20022, Apache-2.0). An administrator adds others — one XSD, or the ZIP a MyStandards or EPC download comes as — and prova keeps them in the data directory under `guidelines/`. Each schema is read when it is added: one prova cannot read, one with no Document, or one for a namespace outside the catalogue is left out, with the reason. Other files in a ZIP (PDFs, spreadsheets) are skipped.

```
prova guideline list pacs.008.001.08
prova guideline add epc-sct-2025 SCT-InterPSP-2025.zip -name "EPC SCT inter-PSP 2025" -publisher "European Payments Council"
prova validate --guideline epc-sct-2025/EPC115-06_2025_V1.0_pacs.008.001.08 payment.xml
prova guideline remove epc-sct-2025
```

The reader follows the constructs guideline schemas use: complex and simple types narrowed by restriction, extensions, type names under any prefix, and GEFEG exports that declare the Document type but no Document element. On EPC and TCH guideline schemas its verdicts agree with libxml2's. prova does not ship the EPC's own schemas, whose terms allow non-commercial reuse only; download them from the EPC and import them.
