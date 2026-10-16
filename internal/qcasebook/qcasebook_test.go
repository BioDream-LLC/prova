package qcasebook

import (
	"fmt"
	"testing"

	"github.com/biodream-llc/prova/internal/verdict"
)

func TestLoadsCompiledCasebook(t *testing.T) {
	b := Default()
	if b.Name != "prova-payments" || b.Version != "v1" {
		t.Fatalf("name/version = %q %q", b.Name, b.Version)
	}
	if len(b.Observations()) != 12 {
		t.Fatalf("observations = %d, want 12", len(b.Observations()))
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
