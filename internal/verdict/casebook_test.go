package verdict

import (
	"strings"
	"testing"
)

func TestControlRuleOutcomes(t *testing.T) {
	bal := func(extra map[string]string) map[string]string {
		m := map[string]string{"availableBalance": "142.00"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	accept := map[string]string{"decision": "accepted"}
	rej := func(code string) map[string]string {
		return map[string]string{"decision": "rejected", "rejectReason": code}
	}

	tests := []struct {
		name string
		req  map[string]string
		ctx  map[string]string
		resp map[string]string
		want Outcome
	}{
		// currency: USD on a EUR account
		{"ccy-accept", map[string]string{"Amount": "90.00", "Currency": "USD"}, bal(map[string]string{"accountCurrency": "EUR"}), accept, Fail},
		{"ccy-reject", map[string]string{"Amount": "90.00", "Currency": "USD"}, bal(map[string]string{"accountCurrency": "EUR"}), rej("AM03"), Pass},
		{"ccy-fx-allowed", map[string]string{"Amount": "90.00", "Currency": "USD"}, bal(map[string]string{"accountCurrency": "EUR", "fxAllowed": "true"}), accept, Pass},
		// duplicate
		{"dup-accept", map[string]string{"Amount": "90.00", "Currency": "EUR", "EndToEndId": "E2E-DUP"}, bal(map[string]string{"duplicate": "true"}), accept, Fail},
		{"dup-reject", map[string]string{"Amount": "90.00", "Currency": "EUR", "EndToEndId": "E2E-DUP"}, bal(map[string]string{"duplicate": "true"}), rej("AM05"), Pass},
		// back-valued
		{"bvd-accept", map[string]string{"Amount": "90.00", "Currency": "EUR"}, bal(map[string]string{"valueDate": "2020-01-01", "settlementDate": "2026-10-15"}), accept, Fail},
		{"bvd-reject", map[string]string{"Amount": "90.00", "Currency": "EUR"}, bal(map[string]string{"valueDate": "2020-01-01", "settlementDate": "2026-10-15"}), rej("DT01"), Pass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Default().Prove(Case{Request: tc.req, Context: tc.ctx, Response: tc.resp})
			if res.Outcome != tc.want {
				t.Fatalf("outcome=%s want %s (chain=%+v)", res.Outcome, tc.want, res.Chain)
			}
		})
	}
}

func TestInsufficientFundsOutcomes(t *testing.T) {
	over := map[string]string{"Amount": "150.00", "Currency": "EUR"}
	within := map[string]string{"Amount": "90.00", "Currency": "EUR"}
	bal := map[string]string{"availableBalance": "142.00", "expectReason": "AM04"}

	rejectAM04 := map[string]string{"decision": "rejected", "rejectReason": "AM04"}
	rejectWrong := map[string]string{"decision": "rejected", "rejectReason": "AC04"}
	accept := map[string]string{"decision": "accepted"}

	tests := []struct {
		name    string
		req     map[string]string
		ctx     map[string]string
		resp    map[string]string
		want    Outcome
		settles string
	}{
		{"over+reject-am04", over, bal, rejectAM04, Pass, ""},
		{"over+accept", over, bal, accept, Fail, ""},
		{"over+reject-wrongreason", over, bal, rejectWrong, Fail, ""},
		{"within+accept", within, bal, accept, Pass, ""},
		{"within+reject", within, bal, rejectAM04, Fail, ""},
		{"over+unknown-balance", over, map[string]string{}, accept, Indeterminate, "context.availableBalance"},
		{"over+accept-maybe-overdraft", over, map[string]string{"availableBalance": "142.00", "overdraftLimit": "unknown"}, accept, Indeterminate, "context.overdraftLimit"},
		{"malformed-no-decision", within, bal, map[string]string{}, Fail, ""},
		{"rejected-without-reason", within, bal, map[string]string{"decision": "rejected"}, Fail, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Default().Prove(Case{Request: tc.req, Context: tc.ctx, Response: tc.resp})
			if res.Outcome != tc.want {
				t.Fatalf("outcome=%s want %s (chain=%+v)", res.Outcome, tc.want, res.Chain)
			}
			if len(res.Chain) == 0 {
				t.Fatal("expected a non-empty proof chain")
			}
			if tc.settles != "" {
				joined := strings.Join(res.Settles, ",")
				if !strings.Contains(joined, tc.settles) {
					t.Fatalf("settles=%q should mention %q", joined, tc.settles)
				}
			}
		})
	}
}

func TestWiderControlOutcomes(t *testing.T) {
	accept := map[string]string{"decision": "accepted"}
	rej := func(code string) map[string]string {
		return map[string]string{"decision": "rejected", "rejectReason": code}
	}
	pay := map[string]string{"Amount": "90.00", "Currency": "EUR"}
	tests := []struct {
		name string
		req  map[string]string
		ctx  map[string]string
		resp map[string]string
		want Outcome
	}{
		{"invalid-bic-accepted", map[string]string{"Amount": "90.00", "Currency": "EUR", "CreditBIC": "BANK-FR"}, map[string]string{"availableBalance": "142.00"}, accept, Fail},
		{"invalid-bic-rejected", map[string]string{"Amount": "90.00", "Currency": "EUR", "CreditBIC": "BANK-FR"}, map[string]string{"availableBalance": "142.00", "expectReason": "RC01"}, rej("RC01"), Pass},
		{"invalid-bic-wrong-reason", map[string]string{"Amount": "90.00", "Currency": "EUR", "CreditBIC": "BANK-FR"}, map[string]string{"availableBalance": "142.00", "expectReason": "RC01"}, rej("AC01"), Fail},
		{"closed-accepted", pay, map[string]string{"availableBalance": "142.00", "accountStatus": "closed"}, accept, Fail},
		{"expired-accepted", map[string]string{"Amount": "90.00", "PAN": "4111111111111111", "Expiry": "2401"}, map[string]string{"availableBalance": "142.00", "processingDate": "2026-10-15"}, accept, Fail},
		{"valid-card-accepted", map[string]string{"Amount": "90.00", "PAN": "4111111111111111", "Expiry": "3012"}, map[string]string{"availableBalance": "142.00", "processingDate": "2026-10-15"}, accept, Pass},
		{"over-limit-accepted", pay, map[string]string{"availableBalance": "142.00", "transactionLimit": "50.00"}, accept, Fail},
		{"return-no-funds-check", pay, map[string]string{"debit": "false"}, accept, Pass},
		{"return-unknown-accepted", pay, map[string]string{"debit": "false", "originalKnown": "false"}, accept, Fail},
		{"return-over-original", map[string]string{"Amount": "120.00", "Currency": "EUR"}, map[string]string{"debit": "false", "originalAmount": "90.00"}, accept, Fail},
		{"cancel-final-accepted", map[string]string{}, map[string]string{"originalFinal": "true"}, accept, Fail},
		{"cancel-pending-refused", map[string]string{}, map[string]string{"originalPending": "true"}, rej("CUST"), Fail},
		{"cancel-pending-done", map[string]string{}, map[string]string{"originalPending": "true"}, accept, Pass},
		{"status-wrong", map[string]string{}, map[string]string{"originalStatus": "PDNG"}, map[string]string{"decision": "accepted", "reportedStatus": "ACSC"}, Fail},
		{"status-right", map[string]string{}, map[string]string{"originalStatus": "PDNG"}, map[string]string{"decision": "accepted", "reportedStatus": "PDNG"}, Pass},
		{"closed-market-accepted", map[string]string{"OrderQty": "9", "Price": "10.00"}, map[string]string{"marketOpen": "false"}, accept, Fail},
		{"short-sale-accepted", map[string]string{"OrderQty": "25", "Side": "sell", "Price": "10.00"}, map[string]string{"position": "20"}, accept, Fail},
		{"sale-within-position", map[string]string{"OrderQty": "15", "Side": "sell", "Price": "10.00"}, map[string]string{"position": "20"}, accept, Pass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Default().Prove(Case{Request: tc.req, Context: tc.ctx, Response: tc.resp})
			if res.Outcome != tc.want {
				t.Fatalf("outcome=%s want %s (chain=%+v)", res.Outcome, tc.want, res.Chain)
			}
		})
	}
}
