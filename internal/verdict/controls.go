package verdict

import (
	"strings"

	"github.com/biodream-llc/prova/internal/af"
)

// The predicates below read one control each from a Case: what was sent (Request), what the tester
// seeded and therefore knows (Context), and what the host returned (Response). Each rule uses its own
// predicate, and RuleExpectedReason uses all of them to decide whether a rejection was required.

func ctx(c Case, k string) string { return strings.TrimSpace(c.Context[k]) }

// fundsApply: the funds rule judges an instruction that carries an amount and debits the account.
// A return, a reversal or a cancellation is seeded with debit=false.
func fundsApply(c Case) bool {
	return strings.TrimSpace(c.Request["Amount"]) != "" && ctx(c, "debit") != "false"
}

func overBalance(c Case) bool {
	return fundsApply(c) && ctx(c, "availableBalance") != "" && minor(c.Request["Amount"]) > minor(ctx(c, "availableBalance"))
}

func ccyMismatch(c Case) bool {
	a := strings.ToUpper(ctx(c, "accountCurrency"))
	r := strings.ToUpper(strings.TrimSpace(c.Request["Currency"]))
	return a != "" && r != "" && a != r && ctx(c, "fxAllowed") != "true"
}

func backValued(c Case) bool {
	vd, sd := ctx(c, "valueDate"), ctx(c, "settlementDate")
	return vd != "" && sd != "" && vd < sd && ctx(c, "backValueAllowed") != "true"
}

func isDuplicate(c Case) bool { return ctx(c, "duplicate") == "true" }

// unusableStatus are the seeded account or card states on which nothing may be accepted.
var unusableStatus = map[string]bool{"closed": true, "blocked": true, "frozen": true, "lost": true, "stolen": true}

// cardExpired compares a card's expiry (Request Expiry, YYMM) with the seeded processing date
// (YYYY-MM-DD). A card is good through the end of its expiry month.
func cardExpired(c Case) bool {
	exp := strings.TrimSpace(c.Request["Expiry"])
	pd := ctx(c, "processingDate")
	if len(exp) != 4 || len(pd) < 7 {
		return false
	}
	return exp < pd[2:4]+pd[5:7]
}

func accountUnusable(c Case) bool {
	return unusableStatus[strings.ToLower(ctx(c, "accountStatus"))] || cardExpired(c)
}

func overLimit(c Case) bool {
	a := strings.TrimSpace(c.Request["Amount"])
	return a != "" && ctx(c, "transactionLimit") != "" && minor(a) > minor(ctx(c, "transactionLimit"))
}

func originalUnknown(c Case) bool { return ctx(c, "originalKnown") == "false" }

func exceedsOriginal(c Case) bool {
	a := strings.TrimSpace(c.Request["Amount"])
	return a != "" && ctx(c, "originalAmount") != "" && minor(a) > minor(ctx(c, "originalAmount"))
}

func originalFinal(c Case) bool   { return ctx(c, "originalFinal") == "true" }
func originalPending(c Case) bool { return ctx(c, "originalPending") == "true" }

func statusMismatch(c Case) bool {
	want := ctx(c, "originalStatus")
	return want != "" && strings.TrimSpace(c.Response["reportedStatus"]) != want
}

func marketClosed(c Case) bool      { return ctx(c, "marketOpen") == "false" }
func instrumentUnknown(c Case) bool { return ctx(c, "instrumentKnown") == "false" }

func isSell(c Case) bool {
	s := strings.ToLower(strings.TrimSpace(c.Request["Side"]))
	return s == "sell" || s == "2"
}

func positionShort(c Case) bool {
	pos := ctx(c, "position")
	if pos == "" || !isSell(c) || ctx(c, "shortSellAllowed") == "true" {
		return false
	}
	return minor(c.Request["OrderQty"]) > minor(pos)
}

// mustReject reports whether any seeded control requires the host to reject the instruction.
func mustReject(c Case) bool {
	return overBalance(c) || ccyMismatch(c) || backValued(c) || isDuplicate(c) || len(FieldProblems(c.Request)) > 0 ||
		accountUnusable(c) || overLimit(c) || originalUnknown(c) || exceedsOriginal(c) || originalFinal(c) ||
		marketClosed(c) || instrumentUnknown(c) || positionShort(c)
}

func accepted(c Case) bool { return c.Response["decision"] == "accepted" }
func rejected(c Case) bool { return c.Response["decision"] == "rejected" }

// mustNotAccept is the shape most controls share: evidence that forbids acceptance, and a violation
// when the host accepted anyway.
func mustNotAccept(id, title string, applies func(Case) bool, evidence, violation func(Case) af.Arg) Rule {
	return Rule{ID: id, Title: title, Apply: func(c Case, b *builder) {
		if !applies(c) {
			return
		}
		b.add(evidence(c))
		if accepted(c) {
			b.violate(violation(c))
		}
	}}
}

// RuleExpectedReason: when a control requires a rejection and the scenario names the reason code the
// standard prescribes, a rejection must carry that code. A host that refuses for the wrong reason
// misleads every system downstream (a closed-account code on a funds failure, say).
func RuleExpectedReason() Rule {
	return Rule{
		ID:    "reason.expected",
		Title: "A required rejection must carry the prescribed reason code",
		Apply: func(c Case, b *builder) {
			want := ctx(c, "expectReason")
			got := c.Response["rejectReason"]
			if want == "" || !rejected(c) || got == want || !mustReject(c) {
				return
			}
			b.violate(af.Arg{ID: "v_reason", Claim: "rejected, but reason " + orDefault(got, "(none)") + " is not the required " + want, Cites: []string{"reject reason " + orDefault(got, "(none)"), "expectReason " + want}})
		},
	}
}

// RuleFieldValidity: an instruction that fails field validation (an unknown currency, an amount with
// too many decimals or not positive, a malformed BIC, an IBAN or card number that fails its checksum, a
// non-positive quantity or price) must be rejected.
func RuleFieldValidity() Rule {
	return Rule{
		ID:    "message.field_validity",
		Title: "An instruction that fails field validation must be rejected",
		Apply: func(c Case, b *builder) {
			probs := FieldProblems(c.Request)
			if len(probs) == 0 {
				return
			}
			b.add(af.Arg{ID: "ev_invalid", Claim: "the instruction fails field validation: " + strings.Join(probs, "; "), Cites: probs})
			if accepted(c) {
				b.violate(af.Arg{ID: "v_invalid", Claim: "host accepted an instruction that fails field validation", Cites: []string{"response decision=accepted"}})
			}
		},
	}
}

// RuleAccountUsable: nothing may be accepted on a closed, blocked or frozen account, or on a card
// reported lost, stolen or past its expiry.
func RuleAccountUsable() Rule {
	return mustNotAccept("account.usable", "Nothing may be accepted on an account or card that cannot be used", accountUnusable,
		func(c Case) af.Arg {
			why := ctx(c, "accountStatus")
			if cardExpired(c) {
				why = "expired " + c.Request["Expiry"] + " (processing " + ctx(c, "processingDate") + ")"
			}
			return af.Arg{ID: "ev_unusable", Claim: "the account or card is " + why, Cites: []string{"seeded account state"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_unusable", Claim: "host accepted an instruction on an account or card that cannot be used", Cites: []string{"response decision=accepted"}}
		})
}

// RuleTransactionLimit: an amount over the seeded per-transaction limit must be rejected, whatever
// the balance.
func RuleTransactionLimit() Rule {
	return mustNotAccept("limit.per_transaction", "An amount over the transaction limit must be rejected", overLimit,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_limit", Claim: "amount " + c.Request["Amount"] + " exceeds the transaction limit " + ctx(c, "transactionLimit"), Cites: []string{"seeded limit"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_limit", Claim: "host accepted an amount over the transaction limit", Cites: []string{"response decision=accepted"}}
		})
}

// RuleOriginalKnown: a return, reversal, cancellation or replacement that refers to an original the
// host never processed must be rejected.
func RuleOriginalKnown() Rule {
	return mustNotAccept("reference.original_known", "An instruction referring to an unknown original must be rejected", originalUnknown,
		func(c Case) af.Arg {
			ref := firstNonEmpty(c.Request["OrgnlEndToEndId"], c.Request["OriginalSTAN"], c.Request["OrigClOrdID"], "the original")
			return af.Arg{ID: "ev_noorig", Claim: ref + " was never processed by the host", Cites: []string{"seeded originalKnown=false"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_noorig", Claim: "host accepted an instruction whose original it never processed", Cites: []string{"response decision=accepted"}}
		})
}

// RuleWithinOriginal: a return or reversal may not move more than the original transaction did.
func RuleWithinOriginal() Rule {
	return mustNotAccept("reference.within_original", "A return or reversal may not exceed the original amount", exceedsOriginal,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_overorig", Claim: "amount " + c.Request["Amount"] + " exceeds the original " + ctx(c, "originalAmount"), Cites: []string{"seeded original amount"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_overorig", Claim: "host accepted more than the original transaction's amount", Cites: []string{"response decision=accepted"}}
		})
}

// RuleCancellation: an original that is already final (settled, returned, filled) cannot be cancelled;
// one that is still pending must be.
func RuleCancellation() Rule {
	return Rule{
		ID:    "cancel.state",
		Title: "A cancellation must follow the original's state",
		Apply: func(c Case, b *builder) {
			if originalFinal(c) {
				b.add(af.Arg{ID: "ev_final", Claim: "the original is already final", Cites: []string{"seeded originalFinal=true"}})
				if accepted(c) {
					b.violate(af.Arg{ID: "v_final", Claim: "host cancelled an original that was already final", Cites: []string{"response decision=accepted"}})
				}
			}
			if originalPending(c) {
				b.add(af.Arg{ID: "ev_pending", Claim: "the original is still pending", Cites: []string{"seeded originalPending=true"}})
				if rejected(c) {
					b.violate(af.Arg{ID: "v_refused", Claim: "host refused to cancel an original that was still pending", Cites: []string{"response decision=rejected", "reason " + orDefault(c.Response["rejectReason"], "(none)")}})
				}
			}
		},
	}
}

// RuleStatusReport: a status report must state the original's known status.
func RuleStatusReport() Rule {
	return Rule{
		ID:    "status.consistent",
		Title: "A status report must state the original's known status",
		Apply: func(c Case, b *builder) {
			want := ctx(c, "originalStatus")
			if want == "" {
				return
			}
			got := strings.TrimSpace(c.Response["reportedStatus"])
			b.add(af.Arg{ID: "ev_status", Claim: "the original's status is " + want, Cites: []string{"seeded originalStatus"}})
			if got != want {
				b.violate(af.Arg{ID: "v_status", Claim: "host reported " + orDefault(got, "no status") + " for an original whose status is " + want, Cites: []string{"reported status " + orDefault(got, "(none)")}})
			}
		},
	}
}

// RuleMarketOpen: a venue must not accept an order while its market is closed or halted.
func RuleMarketOpen() Rule {
	return mustNotAccept("venue.market_open", "No order may be accepted while the market is closed", marketClosed,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_closed", Claim: "the market is closed", Cites: []string{"seeded marketOpen=false"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_closed", Claim: "host accepted an order while the market was closed", Cites: []string{"response decision=accepted"}}
		})
}

// RuleInstrumentListed: a venue must not accept an order for an instrument it does not list.
func RuleInstrumentListed() Rule {
	return mustNotAccept("venue.instrument_listed", "No order may be accepted for an unlisted instrument", instrumentUnknown,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_unlisted", Claim: c.Request["Symbol"] + " is not listed on the venue", Cites: []string{"seeded instrumentKnown=false"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_unlisted", Claim: "host accepted an order for an instrument it does not list", Cites: []string{"response decision=accepted"}}
		})
}

// RulePosition: without a short-selling facility, a sale may not exceed the position held.
func RulePosition() Rule {
	return mustNotAccept("venue.position", "A sale may not exceed the position held", positionShort,
		func(c Case) af.Arg {
			return af.Arg{ID: "ev_short", Claim: "selling " + c.Request["OrderQty"] + " against a position of " + ctx(c, "position"), Cites: []string{"seeded position"}}
		},
		func(c Case) af.Arg {
			return af.Arg{ID: "v_short", Claim: "host accepted a sale larger than the position", Cites: []string{"response decision=accepted"}}
		})
}
