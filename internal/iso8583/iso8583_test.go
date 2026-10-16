package iso8583

import "testing"

func TestPackUnpackRoundTrip(t *testing.T) {
	raw, err := Encode(Request{PAN: "4111111111111111", Amount: "150.00", Currency: "EUR", STAN: "000101", Terminal: "TERM0001"})
	if err != nil {
		t.Fatal(err)
	}
	mti, fields, err := Unpack(raw)
	if err != nil {
		t.Fatal(err)
	}
	if mti != "0100" {
		t.Fatalf("MTI = %q, want 0100", mti)
	}
	if fields[2] != "4111111111111111" {
		t.Fatalf("DE2 PAN wrong: %q", fields[2])
	}
	if fields[4] != "000000015000" {
		t.Fatalf("DE4 amount wrong: %q (want 15000 minor units)", fields[4])
	}
	if fields[49] != "978" {
		t.Fatalf("DE49 currency wrong: %q (want 978 EUR)", fields[49])
	}
}

func TestResponseRoundTrip(t *testing.T) {
	req := map[int]string{3: "000000", 4: "000000015000", 11: "000101", 49: "978"}
	raw, err := EncodeResponse(req, "51")
	if err != nil {
		t.Fatal(err)
	}
	st, err := DecodeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if st.MTI != "0110" || st.ResponseCode != "51" {
		t.Fatalf("response wrong: %+v", st)
	}
	f := st.Fields()
	if f["decision"] != "rejected" || f["rejectReason"] != "51" {
		t.Fatalf("canonical fields wrong: %v", f)
	}
}

func TestSimDecide(t *testing.T) {
	s := &Sim{Ledger: map[string]int64{"4111111111111111": 14200}}
	over := s.Decide(map[int]string{2: "4111111111111111", 4: "000000015000"})
	if over != "51" {
		t.Fatalf("over-limit should decline 51, got %q", over)
	}
	ok := s.Decide(map[int]string{2: "4111111111111111", 4: "000000009000"})
	if ok != "00" {
		t.Fatalf("within-limit should approve 00, got %q", ok)
	}
	buggy := &Sim{Ledger: map[string]int64{"4111111111111111": 14200}, Mode: "approve-all"}
	if buggy.Decide(map[int]string{2: "4111111111111111", 4: "000000015000"}) != "00" {
		t.Fatal("approve-all switch should wrongly approve over-limit")
	}
}
