package verdict

import (
	"strings"

	"github.com/biodream-llc/prova/internal/af"
	"github.com/biodream-llc/prova/internal/cardcrypto"
)

// Card security verified under the tester's seeded test keys. The format checks (a PIN block is hex of
// the right length, chip data is well-formed TLV with a cryptogram) are field validation; these go
// further and prove the PIN, MAC or cryptogram is wrong, so a host that approved it is provably at
// fault. Each check runs only when its key is seeded:
//
//	pinKey + cardPIN (+ pinFormat)          the PIN block (DE52) must decipher to the card's PIN
//	macKey (+ macAlgorithm)                 the MAC (DE64) must be right over prova's MAC fields
//	imk (+ cryptogramVersion, psn)          the ARQC in the chip data (DE55 tag 9F26) must verify
//
// prova's second engine (internal/qcasebook) carries its own implementation of the same checks.
func securityProblems(c Case) []string {
	r := c.Request
	var out []string
	pan := strings.TrimSpace(r["PAN"])
	if key, pin, block := ctx(c, "pinKey"), ctx(c, "cardPIN"), strings.TrimSpace(r["PINBlock"]); key != "" && pin != "" && block != "" {
		k, err1 := cardcrypto.Hex(key)
		b, err2 := cardcrypto.Hex(block)
		f, err3 := cardcrypto.ParseFormat(ctx(c, "pinFormat"))
		switch {
		case err1 != nil || err2 != nil || err3 != nil:
			out = append(out, "the PIN block cannot be deciphered with the seeded PIN key")
		default:
			got, err := cardcrypto.DecipherPIN(f, k, b, pan)
			if err != nil {
				out = append(out, "the PIN block does not decipher to a valid PIN under the seeded key")
			} else if got != pin {
				out = append(out, "the PIN block carries a PIN other than the card's")
			}
		}
	}
	if key, mac := ctx(c, "macKey"), strings.TrimSpace(r["MAC"]); key != "" && mac != "" {
		k, err := cardcrypto.Hex(key)
		data := cardcrypto.MACData(pan, r["ProcessingCode"], r["Amount"], r["Currency"], r["STAN"], r["Terminal"])
		ok := false
		if err == nil {
			ok, err = cardcrypto.VerifyMAC(ctx(c, "macAlgorithm"), k, data, mac)
		}
		if err != nil || !ok {
			out = append(out, "the MAC does not verify under the seeded MAC key")
		}
	}
	if imk, icc := ctx(c, "imk"), strings.TrimSpace(r["ICCData"]); imk != "" && icc != "" {
		k, err := cardcrypto.Hex(imk)
		ok := false
		if err == nil {
			ver := ctx(c, "cryptogramVersion")
			if ver == "" {
				ver = cardcrypto.VisaCVN10
			}
			ok, err = cardcrypto.VerifyARQC(ver, k, pan, ctx(c, "psn"), icc)
		}
		if err != nil || !ok {
			out = append(out, "the chip cryptogram (ARQC) does not verify under the seeded issuer key")
		}
	}
	return out
}

func securityFailed(c Case) bool { return len(securityProblems(c)) > 0 }

// RuleCardSecurity: a PIN, MAC or cryptogram that fails verification under the seeded keys must be
// declined.
func RuleCardSecurity() Rule {
	return Rule{ID: "card.security_verified", Title: "A PIN, MAC or cryptogram that fails verification must be declined",
		Apply: func(c Case, b *builder) {
			probs := securityProblems(c)
			if len(probs) == 0 {
				return
			}
			b.add(af.Arg{ID: "ev_insecure", Claim: strings.Join(probs, "; "), Cites: append([]string{"seeded test keys"}, probs...)})
			if accepted(c) {
				b.violate(af.Arg{ID: "v_insecure", Claim: "host approved a transaction whose PIN, MAC or cryptogram fails verification", Cites: []string{"response decision=accepted"}})
			}
		}}
}

func mandateMissing(c Case) bool { return ctx(c, "mandateValid") == "false" }

// RuleMandate: a direct debit with no valid mandate must be refused.
func RuleMandate() Rule {
	return mustNotAccept("mandate.valid", "A direct debit without a valid mandate must be refused", mandateMissing,
		func(c Case) af.Arg {
			m := orDefault(c.Request["MandateId"], "(none)")
			return af.Arg{ID: "ev_nomandate", Claim: "the debtor gave no valid mandate " + m, Cites: []string{"seeded mandateValid=false"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_nomandate", Claim: "host collected a direct debit without a valid mandate", Cites: []string{"response decision=accepted"}}
		})
}

func sessionViolation(c Case) bool { return ctx(c, "sessionViolation") == "true" }

// RuleSession: a message that breaks the FIX session rules (a sequence number too low, a possible
// duplicate without its original sending time, a sequence reset backwards, a message type the session
// version does not have) must not be processed.
func RuleSession() Rule {
	return mustNotAccept("session.rules", "A message that breaks the FIX session rules must not be processed", sessionViolation,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_session", Claim: "the message breaks the FIX session rules: " + orDefault(ctx(c, "sessionRule"), "see the scenario"), Cites: []string{"seeded sessionViolation=true"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_session", Claim: "venue processed a message that breaks the session rules", Cites: []string{"response decision=accepted"}}
		})
}

// reportMismatch reports whether an account report (camt.052/053/054) disagrees with what the tester
// seeded: the account it is for, its closing balance, or how many entries it lists.
func reportMismatch(c Case) []string {
	var out []string
	resp := c.Response
	if want := ctx(c, "statementAccount"); want != "" && strings.TrimSpace(resp["reportedAccount"]) != want {
		out = append(out, "the report is for account "+orDefault(resp["reportedAccount"], "(none)")+", not "+want)
	}
	if want := ctx(c, "statementBalance"); want != "" {
		got := strings.TrimSpace(resp["reportedBalance"])
		if got == "" || minor(got) != minor(want) {
			out = append(out, "the closing balance is "+orDefault(got, "(none)")+", not "+want)
		}
	}
	if want := ctx(c, "statementEntries"); want != "" && strings.TrimSpace(resp["entries"]) != want {
		out = append(out, "the report lists "+orDefault(resp["entries"], "no")+" entries, not "+want)
	}
	return out
}
