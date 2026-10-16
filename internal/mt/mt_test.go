package mt

import (
	"strings"
	"testing"

	"github.com/biodream-llc/prova/internal/iso20022"
)

const sampleMT103 = `{1:F01BANKDEFFAXXX0000000000}{2:I103BANKFRPPAXXXN}{4:
:20:E2E-MT-1
:23B:CRED
:32A:261015EUR90,00
:52A:BANKDEFFXXX
:50K:/ACC-EUR-1
JOHN DOE
:57A:BANKFRPPXXX
:59:/ACC-FR-9
JANE ROE
:71A:SHA
-}`

func TestMT103ToPacs008(t *testing.T) {
	req, err := MT103ToPacs008(sampleMT103)
	if err != nil {
		t.Fatal(err)
	}
	if req.Amount != "90.00" || req.Currency != "EUR" || req.ValueDate != "2026-10-15" {
		t.Fatalf("amount/ccy/date wrong: %+v", req)
	}
	if req.DebtorAcct != "ACC-EUR-1" || req.CreditAcct != "ACC-FR-9" {
		t.Fatalf("accounts wrong: %+v", req)
	}
	if req.DebtorBIC != "BANKDEFFXXX" || req.CreditBIC != "BANKFRPPXXX" {
		t.Fatalf("agents wrong: %+v", req)
	}
	if req.EndToEndId != "E2E-MT-1" {
		t.Fatalf("ref wrong: %+v", req)
	}
}

func TestRoundTrip(t *testing.T) {
	orig := iso20022.Request{
		MsgId: "E2E-RT", EndToEndId: "E2E-RT", Amount: "150.00", Currency: "EUR",
		ValueDate: "2026-10-15", DebtorAcct: "ACC-EUR-1", CreditAcct: "ACC-FR-9",
		DebtorBIC: "BANKDEFFXXX", CreditBIC: "BANKFRPPXXX",
	}
	mt, err := Pacs008ToMT103(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mt, ":32A:261015EUR150,00") {
		t.Fatalf("MT :32A: not rendered: %s", mt)
	}
	back, err := MT103ToPacs008(mt)
	if err != nil {
		t.Fatal(err)
	}
	if back.Amount != orig.Amount || back.Currency != orig.Currency || back.ValueDate != orig.ValueDate {
		t.Fatalf("round-trip amount/ccy/date drift: %+v vs %+v", back, orig)
	}
	if back.DebtorAcct != orig.DebtorAcct || back.CreditAcct != orig.CreditAcct {
		t.Fatalf("round-trip account drift: %+v", back)
	}
}
