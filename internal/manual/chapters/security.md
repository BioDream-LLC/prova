# Security

prova is built to be put in front of people who handle payment systems, so its defaults are the strict ones.

## In transit

HTTPS only, TLS 1.2 and later. Every response carries `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`. API responses are marked `Cache-Control: no-store`, so a shared machine's browser cache holds no verdicts.

## Requests that change something

A request that changes anything must be JSON from prova's own origin. A form on another site cannot send JSON, and a script on another site cannot without a cross-origin preflight that prova never grants; prova also checks `Origin` and `Sec-Fetch-Site`. So a signed-in browser visiting a hostile page cannot be made to run a suite or create an account. The single exception is the SAML provider's form POST, which is protected instead by its signature and by answering a sign-in prova started.

## At rest

| What | How it is kept |
|---|---|
| passwords | salted PBKDF2-SHA256, 600,000 iterations; generated, shown once |
| session tokens | never stored; the database holds their SHA-256 |
| the database | created readable by its owner only |
| OIDC client secret | read from a file or the environment, never the command line |

## Scripts

Scenario scripts run in a sandbox with no file system, network, process or module access, and a hard time limit. A script can describe messages; it cannot reach anything.

## What prova sends where

prova connects to the targets you point it at, to your identity provider when single sign-on is configured, and nowhere else. It has no telemetry and no update check.

## Reporting a vulnerability

Report it privately through the **Security** tab of the prova repository on GitHub (“Report a vulnerability”). Please do not open a public issue for a security problem.
