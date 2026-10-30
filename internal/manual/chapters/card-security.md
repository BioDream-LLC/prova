# Card security

A card switch must check more than the balance. It must decline a wrong PIN, a message altered in transit and a counterfeit chip — and a switch that approves one returns a perfectly valid 0110. prova sends PIN blocks, MACs and chip cryptograms computed from known test keys, so it can prove what the switch should have decided.

## Format and verification

prova checks card security at two levels, in both verdict engines:

- **Format.** A PIN block must be 16 hex characters (32 for format 4), a MAC 16 or 32, chip data well-formed BER-TLV carrying an Application Cryptogram (tag `9F26`), and a chip entry (DE22 `05x` or `07x`) must carry chip data at all. A malformed message is declined with DE39 `30`.
- **Verification.** When the scenario seeds the keys a tester holds, prova deciphers the PIN block, recomputes the MAC and the cryptogram, and compares. A failure that the switch approved is a provable defect.

| Seeded in the context | Checks |
|---|---|
| `pinKey`, `cardPIN`, optionally `pinFormat` | the PIN block (DE52) deciphers to the card's PIN |
| `macKey`, optionally `macAlgorithm` | the MAC (DE64) is right over prova's MAC fields |
| `imk`, optionally `cryptogramVersion` and `psn` | the cryptogram in the chip data (DE55 `9F26`) verifies |

The simulated switches decline a wrong PIN with `55`, a MAC failure with `63` and a cryptogram failure with `63` in the generic dialect, `82` in the Visa-style dialect and `88` in the Mastercard-style dialect.

## What prova computes

| | Supported | Checked against |
|---|---|---|
| **PIN blocks** (ISO 9564-1) | format 0 (PIN XOR PAN), 1 (no PAN), 3 (random A–F fill) under Triple DES; format 4 under AES | psec |
| **MACs** | ISO 9797-1 algorithm 3 (retail, ANSI X9.19) and algorithm 1 (CBC-MAC) under DES/Triple DES; AES-CMAC (NIST SP 800-38B) | psec; the RFC 4493 examples; Python's cryptography |
| **Cryptograms** (EMV) | Visa CVN 10 (option A master key, no session key), Visa CVN 18 (option B master key, EMV common session key), Mastercard CVN 16 (Mastercard session key from the ATC and unpredictable number) | pyemv |
| **Card values** | Visa PVV, CVV and CVC, key check values | psec |

The checks against the oracles run in prova's test suite on vectors computed by those libraries; none of them ships with prova. prova composes the block ciphers in Go's standard library and implements no cipher of its own. The second engine has its own implementation of every check — chaining blocks, assembling PIN fields and walking the TLV by a different construction — and the two are compared on more than six thousand generated cases.

### The MAC fields

prova MACs a field selection, as ANSI X9.19 allows: the card number, processing code, amount (with two decimals), alphabetic currency, STAN and terminal id, each followed by a `0x1C` separator. A network that MACs the whole message computes something different; for such a host, leave `macKey` unseeded and prova checks only the MAC's format.

### The test keys

The keys and cards the simulated switches use are published, so anyone can reproduce every PIN block, MAC and cryptogram prova sends. They are shown on the **Card security** screen and at `GET /api/cardcrypto/testkeys`. They are test values: never enter a production key into prova.

| | Value |
|---|---|
| PIN key (Triple DES) | `0123456789ABCDEFFEDCBA9876543210` |
| PIN key (AES, format 4) | `00112233445566778899AABBCCDDEEFF` |
| MAC key (retail MAC) | `89ABCDEF0123456776543210FEDCBA98` |
| MAC key (AES-CMAC) | `2B7E151628AED2A6ABF7158809CF4F3C` |
| Issuer master key | `11223344556677888877665544332211` |

| Card | PIN | Cryptogram |
|---|---|---|
| 4111 1111 1111 1111 | 1234 | Visa CVN 10 |
| 4012 8888 8888 1881 | 4321 | Visa CVN 18 |
| 5105 1051 0510 5100 | 2580 | Mastercard CVN 16 |

## The Card security screen

The screen builds and checks each value with the test keys: encipher and decipher a PIN block, compute a MAC over the MAC fields, build chip data with a correct cryptogram and verify chip data, and compute a PVV, CVV or key check value. Viewers may use it: it computes and stores nothing.

## HSMs

prova talks to HSMs that speak the Thales payShield host commands: a two-byte length, the message header the HSM is configured with, a two-character command and fixed fields over TCP. Keys travel as the HSM holds them, encrypted under its local master key; prova never sees them in clear.

| Command | prova |
|---|---|
| `NC` diagnostics | the LMK check value and firmware version |
| `CA` translate a PIN block from a terminal key to a zone key | `prova hsm translate-pin` |
| `EC` verify a PIN under a zone key against a Visa PVV | `prova hsm verify-pin` |
| `CY` verify a CVV or CVC | `prova hsm verify-cvv` |

```
prova hsm --addr 10.0.0.5:1500 --header SSSS diagnostics
prova hsm --addr 10.0.0.5:1500 verify-pin U<ZPK> <PVK-PAIR> <PIN-BLOCK> 0 4111111111111111 1 <PVV>
```

Start the server with `-hsm host:port` (and `-hsm-header`) and the screen's HSM tab checks it. The command layouts follow the payShield 9000/10K host command reference and are tested against the open-source pythales simulator; check them on your HSM before relying on them.
