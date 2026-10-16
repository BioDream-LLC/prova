package iso20022

import "testing"

func TestRequestRoundTrip(t *testing.T) {
	req := Request{
		MsgId: "MSG-1", EndToEndId: "E2E-1", Amount: "150.00", Currency: "EUR",
		DebtorAcct: "ACC-EUR-1", CreditAcct: "ACC-FR-9", DebtorBIC: "BANKDEFFXXX", CreditBIC: "BANKFRPPXXX",
	}
	raw, err := Encode(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != "150.00" || got.Currency != "EUR" || got.EndToEndId != "E2E-1" || got.DebtorAcct != "ACC-EUR-1" {
		t.Fatalf("round trip lost fields: %+v", got)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	raw, err := EncodeStatus(Status{MsgId: "RSP-1", OrgnlEndToEndId: "E2E-1", TxSts: "RJCT", ReasonCode: "AM04"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := DecodeStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if st.TxSts != "RJCT" || st.ReasonCode != "AM04" || st.OrgnlEndToEndId != "E2E-1" {
		t.Fatalf("status round trip wrong: %+v", st)
	}
	if st.Fields()["RjctRsn"] != "AM04" {
		t.Fatalf("fields missing reason: %v", st.Fields())
	}
}

func TestSimDecide(t *testing.T) {
	s := &Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}}
	over := s.Decide(Request{DebtorAcct: "ACC-EUR-1", Amount: "150.00", EndToEndId: "x"})
	if over.TxSts != "RJCT" || over.ReasonCode != "AM04" {
		t.Fatalf("compliant host should reject over-balance with AM04: %+v", over)
	}
	ok := s.Decide(Request{DebtorAcct: "ACC-EUR-1", Amount: "90.00", EndToEndId: "y"})
	if ok.TxSts != "ACSP" {
		t.Fatalf("compliant host should accept within balance: %+v", ok)
	}
	buggy := &Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}, Mode: "accept-all"}
	if buggy.Decide(Request{DebtorAcct: "ACC-EUR-1", Amount: "150.00"}).TxSts != "ACSP" {
		t.Fatal("accept-all host should (wrongly) accept over-balance")
	}
}

func TestMinorUnits(t *testing.T) {
	cases := map[string]int64{"150.00": 15000, "90": 9000, "0.5": 50, "142.00": 14200, "-1.00": -100}
	for in, want := range cases {
		if got := minorUnits(in); got != want {
			t.Errorf("minorUnits(%q)=%d want %d", in, got, want)
		}
	}
}
