# Getting Started

## Running it

prova is one file. The web interface, this manual and the database engine are inside it. There is nothing to install and no service to set up first.

```
./prova
```

It listens on `https://localhost:8443`. On first start it prints two things to its log that you need:

```
prova: first start — created the administrator account
prova:     name:     admin
prova:     password: (twenty generated characters)
prova: self-signed TLS certificate …/prova/tls/cert.pem (SHA-256 3F:A1:…)
```

The password is shown once. prova keeps only its hash; if you lose it, `prova user reset admin` issues a new one.

> Without a certificate of its own, prova makes a self-signed one and keeps it, so the browser asks you to trust it the first time and then remembers. For anything other than your own machine, give it a real certificate with `-tls-cert` and `-tls-key`; see [Deployment and Operation](#deployment-and-operation).

## Signing in

Open `https://localhost:8443` and sign in as `admin` with the password from the log. The first screen is the dashboard, and it already has verdicts on it: on a fresh installation prova drives twenty scenarios at its built-in simulated targets, so every screen has something real to show. They are real runs, proved like any other.

## The first verdict

1. Open **Scenarios**.
2. Choose **pacs.008 over balance**. The request is a €150.00 credit transfer; the context says the debtor account holds €142.00.
3. Choose the target **cbpr-uat**, a compliant simulated ISO 20022 host, and press **Run**.

The host rejects the payment with reason `AM04` (insufficient funds), and prova's verdict is **pass**: the host did what the funds-sufficiency rule requires. Open the run on the **Proof** screen to see why — the argument that the host must reject, the evidence that it did, and the certificate from the independent re-check.

## The first failed proof

Run the same scenario again, against **switch-sim**. This simulated host has a planted defect: it accepts everything.

It answers `ACSC` — a well-formed acceptance that any format validator passes — and prova's verdict is **fail**. The Proof screen shows the funds-sufficiency rule defeating the claim that the host behaved correctly, and cites the evidence: €150.00 instructed against €142.00 available.

That is the whole idea of prova in two runs. The same well-formed response is right from one host and wrong from another, and only the evidence of the exchange can tell which.

## What to do next

- Run the **ISO 20022 regression** suite on the Suites screen, then against `switch-sim`, and compare.
- Read [Verdicts and Proofs](#verdicts-and-proofs) to understand what the Proof screen shows.
- Write a scenario of your own: [Scenarios and Scripting](#scenarios-and-scripting).
- Add your colleagues: [Accounts and Signing In](#accounts-and-signing-in).
- Put a suite in your pipeline: [The Command Line](#the-command-line).
