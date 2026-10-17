package qcasebook

import (
	"fmt"
	"testing"

	"github.com/biodream-llc/prova/internal/verdict"
)

func TestLoadsCompiledCasebook(t *testing.T) {
	b := Default()
	if b.Name != "prova-payments" || b.Version != "v2" {
		t.Fatalf("name/version = %q %q", b.Name, b.Version)
	}
	if len(b.Observations()) != 23 {
		t.Fatalf("observations = %d, want 23", len(b.Observations()))
	}
	if b.Fingerprint == "" {
		t.Fatal("missing cbc fingerprint")
	}
}

func TestBindsEveryDeclaredObservation(t *testing.T) {
	got := Bind(verdict.Case{})
	for _, o := range Default().Observations() {
		if _, ok := got[o]; !ok {
			t.Errorf("binding never sets declared observation %q", o)
		}
	}
	if len(got) != len(Default().Observations()) {
		t.Errorf("binding sets %d readings, casebook declares %d", len(got), len(Default().Observations()))
	}
}

func TestScenarioVerdicts(t *testing.T) {
	b := Default()
	cases := []struct {
		name string
		c    verdict.Case
		want verdict.Outcome
	}{
		{"over+reject", verdict.Case{
			Request:  map[string]string{"Amount": "150.00", "Currency": "EUR"},
			Context:  map[string]string{"availableBalance": "142.00", "expectReason": "AM04"},
			Response: map[string]string{"decision": "rejected", "rejectReason": "AM04"}}, verdict.Pass},
		{"over+accept", verdict.Case{
			Request:  map[string]string{"Amount": "150.00", "Currency": "EUR"},
			Context:  map[string]string{"availableBalance": "142.00"},
			Response: map[string]string{"decision": "accepted"}}, verdict.Fail},
		{"unknown-balance", verdict.Case{
			Request:  map[string]string{"Amount": "150.00", "Currency": "EUR"},
			Context:  map[string]string{},
			Response: map[string]string{"decision": "accepted"}}, verdict.Indeterminate},
		{"duplicate-accepted", verdict.Case{
			Request:  map[string]string{"Amount": "90.00", "Currency": "EUR"},
			Context:  map[string]string{"availableBalance": "142.00", "duplicate": "true"},
			Response: map[string]string{"decision": "accepted"}}, verdict.Fail},
	}
	for _, tc := range cases {
		if got := b.Prove(tc.c).Outcome; got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// TestAgreesWithGoCasebook is the cross-engine equivalence check: across every combination of the
// dimensions the rules read, quorum's compiled casebook and prova's Go casebook must reach the same
// verdict. The rules were written twice, in two languages, by two constructions; agreement here is
// corroboration, and any disagreement names the exact case that breaks it.
func TestAgreesWithGoCasebook(t *testing.T) {
	q := Default()
	g := verdict.Default()
	n, bad := 0, 0
	for _, decision := range []string{"accepted", "rejected", ""} {
		for _, reason := range []string{"", "AM04", "AC04", "AM03", "AM05", "51"} {
			if decision != "rejected" && reason != "" {
				continue
			}
			for _, amt := range []string{"90.00", "142.00", "150.00"} {
				for _, bal := range []string{"142.00", ""} {
					for _, expect := range []string{"", "AM04"} {
						for _, ccy := range [][2]string{{"", "EUR"}, {"EUR", "EUR"}, {"EUR", "USD"}} {
							for _, dup := range []string{"", "true"} {
								for _, dates := range [][2]string{{"", ""}, {"2026-10-15", "2026-10-15"}, {"2020-01-01", "2026-10-15"}} {
									for _, od := range []string{"", "unknown"} {
										ctx := map[string]string{}
										set := func(k, v string) {
											if v != "" {
												ctx[k] = v
											}
										}
										set("availableBalance", bal)
										set("expectReason", expect)
										set("accountCurrency", ccy[0])
										set("duplicate", dup)
										set("valueDate", dates[0])
										set("settlementDate", dates[1])
										set("overdraftLimit", od)
										resp := map[string]string{}
										set2 := func(k, v string) {
											if v != "" {
												resp[k] = v
											}
										}
										set2("decision", decision)
										set2("rejectReason", reason)
										c := verdict.Case{
											Request:  map[string]string{"Amount": amt, "Currency": ccy[1]},
											Context:  ctx,
											Response: resp,
										}
										n++
										gq, gg := q.Prove(c).Outcome, g.Prove(c).Outcome
										if gq != gg {
											bad++
											if bad <= 10 {
												t.Errorf("disagree: quorum=%s go=%s case=%s", gq, gg, fmt.Sprint(c))
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d cases compared, %d disagreements", n, bad)
}

// TestAgreesOnTheWiderControls compares the two engines across every combination of the controls
// added beyond funds, currency, dates and duplicates: field validity, account state, the transaction
// limit, references to originals, cancellation state, status reports and the venue controls — each
// crossed with whether the funds rule applies, every kind of response and an expected reason code.
func TestAgreesOnTheWiderControls(t *testing.T) {
	q := Default()
	g := verdict.Default()
	n, bad := 0, 0
	for mask := 0; mask < 1<<10; mask++ {
		on := func(i int) bool { return mask&(1<<i) != 0 }
		for _, status := range []string{"", "ACSC", "PDNG"} {
			for _, funds := range []string{"debit=false", "200.00", "100.00"} {
				for _, resp := range []map[string]string{
					{"decision": "accepted"}, {"decision": "accepted", "reportedStatus": "ACSC"},
					{"decision": "rejected"}, {"decision": "rejected", "rejectReason": "X1"}, {"decision": "rejected", "rejectReason": "AM04"},
					{},
				} {
					for _, expect := range []string{"", "X1"} {
						req := map[string]string{"Amount": "150.00", "Currency": "EUR", "Side": "buy", "OrderQty": "15"}
						ctx := map[string]string{}
						if funds == "debit=false" {
							ctx["debit"] = "false"
						} else {
							ctx["availableBalance"] = funds
						}
						if on(0) {
							req["Currency"] = "XYZ"
						}
						if on(1) {
							ctx["accountStatus"] = "closed"
						}
						if on(2) {
							ctx["transactionLimit"] = "100.00"
						} else {
							ctx["transactionLimit"] = "500.00"
						}
						if on(3) {
							ctx["originalKnown"] = "false"
						}
						if on(4) {
							ctx["originalAmount"] = "100.00"
						}
						if on(5) {
							ctx["originalFinal"] = "true"
						}
						if on(6) {
							ctx["originalPending"] = "true"
						}
						if on(7) {
							ctx["marketOpen"] = "false"
						}
						if on(8) {
							ctx["instrumentKnown"] = "false"
						}
						if on(9) {
							req["Side"] = "sell"
							ctx["position"] = "10"
						}
						if status != "" {
							ctx["originalStatus"] = status
						}
						if expect != "" {
							ctx["expectReason"] = expect
						}
						c := verdict.Case{Request: req, Context: ctx, Response: resp}
						n++
						gq, gg := q.Prove(c).Outcome, g.Prove(c).Outcome
						if gq != gg {
							bad++
							if bad <= 10 {
								t.Errorf("disagree: quorum=%s go=%s case=%s", gq, gg, fmt.Sprint(c))
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d cases compared, %d disagreements", n, bad)
}

// TestFieldValidationAgrees compares the two engines' separately written field checks on valid and
// invalid identifiers, amounts and quantities.
func TestFieldValidationAgrees(t *testing.T) {
	g := verdict.Default()
	reqs := []map[string]string{
		{"Amount": "90.00", "Currency": "EUR"}, {"Amount": "0.00"}, {"Amount": "-1.00"}, {"Amount": "1.234", "Currency": "EUR"},
		{"Amount": "1.234", "Currency": "BHD"}, {"Amount": "100", "Currency": "JPY"}, {"Amount": "100.5", "Currency": "JPY"},
		{"Amount": "abc"}, {"Amount": "1e3"}, {"Amount": "1234567890123456789"}, {"Amount": ".5"}, {"Amount": "5."},
		{"Currency": "XYZ"}, {"Currency": "eur"}, {"DebtorBIC": "BANKDEFFXXX"}, {"DebtorBIC": "BANKDEFF"}, {"DebtorBIC": "BANK-DE-FF"},
		{"CreditBIC": "bankdeff"}, {"CreditBIC": "BANKD1FF"}, {"DebtorIBAN": "DE89370400440532013000"}, {"DebtorIBAN": "DE00370400440532013000"},
		{"CreditIBAN": "FR1420041010050500013M02606"}, {"CreditIBAN": "GB82 WEST 1234 5698 7654 32"}, {"CreditIBAN": "GB82WEST1234569876543"},
		{"PAN": "4111111111111111"}, {"PAN": "4111111111111112"}, {"PAN": "41111111111"}, {"PAN": "411111111111111a"},
		{"OrderQty": "15"}, {"OrderQty": "0"}, {"OrderQty": "-3"}, {"OrderQty": "1.5"}, {"OrderQty": "+5"}, {"OrderQty": ""},
		{"Price": "10.00"}, {"Price": "0"}, {"Price": "-1"}, {"Price": "x"},
	}
	for _, r := range reqs {
		goInvalid := len(verdict.FieldProblems(r)) > 0
		if qInvalid := invalidInstruction(r); goInvalid != qInvalid {
			t.Errorf("%v: go invalid=%v, quorum invalid=%v", r, goInvalid, qInvalid)
		}
		c := verdict.Case{Request: r, Context: map[string]string{"debit": "false"}, Response: map[string]string{"decision": "accepted"}}
		if gq, gg := Default().Prove(c).Outcome, g.Prove(c).Outcome; gq != gg {
			t.Errorf("%v: quorum=%s go=%s", r, gq, gg)
		}
	}
}
