package synth

import (
	"strconv"
	"testing"
)

func TestPANLuhn(t *testing.T) {
	g := New(1)
	for i := 0; i < 100; i++ {
		pan := g.PAN("4")
		if len(pan) != 16 {
			t.Fatalf("PAN length = %d, want 16: %s", len(pan), pan)
		}
		if !LuhnValid(pan) {
			t.Fatalf("PAN %s fails Luhn", pan)
		}
	}
}

func TestIBANChecksum(t *testing.T) {
	g := New(2)
	for i := 0; i < 100; i++ {
		iban := g.IBAN("DE")
		if !IBANValid(iban) {
			t.Fatalf("IBAN %s fails mod-97", iban)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	for i := 0; i < 20; i++ {
		if a.PAN("5") != b.PAN("5") || a.IBAN("FR") != b.IBAN("FR") {
			t.Fatal("same seed must produce the same stream")
		}
	}
}

func TestAmountRange(t *testing.T) {
	g := New(3)
	for i := 0; i < 100; i++ {
		v, err := strconv.ParseFloat(g.Amount(10, 20), 64)
		if err != nil || v < 10 || v > 20 {
			t.Fatalf("amount out of range or unparseable: %v (%v)", v, err)
		}
	}
}

func TestBICShape(t *testing.T) {
	bic := New(4).BIC("GB")
	if len(bic) != 11 || bic[4:6] != "GB" {
		t.Fatalf("BIC malformed: %s", bic)
	}
}
