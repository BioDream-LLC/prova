package validate

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/biodream-llc/prova/internal/iso20022"
)

// TestEverythingProvaSendsIsSchemaValid encodes every ISO 20022 message prova can send, and every reply
// its simulated host can give, and checks each against the official schema. A test tool that sends
// schema-invalid messages would blame the host for its own mistakes.
func TestEverythingProvaSendsIsSchemaValid(t *testing.T) {
	base := map[string]string{
		"MsgId": "MSG-1", "EndToEndId": "E2E-1", "Amount": "90.00", "Currency": "EUR",
		"DebtorAcct": "ACC-EUR-1", "CreditAcct": "ACC-FR-9", "DebtorBIC": "BANKDEFFXXX", "CreditBIC": "BANKFRPPXXX",
		"OrgnlEndToEndId": "E2E-ORIG", "Reason": "AC03", "ValueDate": "2026-10-15",
		"DebtorIBAN": "DE89370400440532013000", "CreditIBAN": "FR1420041010050500013M02606",
		"MandateId": "MNDT-1", "Account": "ACC-EUR-1",
	}
	for _, kind := range iso20022.RequestKinds() {
		rec := map[string]string{"Message": kind}
		for k, v := range base {
			rec[k] = v
		}
		if kind == iso20022.KindCamt056 {
			rec["Reason"] = "DUPL"
		}
		raw, err := iso20022.Encode(iso20022.FromRecord(rec))
		if err != nil {
			t.Fatalf("%s: encode: %v", kind, err)
		}
		check(t, kind, raw)
	}
	sim := &iso20022.Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}, Originals: map[string]iso20022.Original{"E2E-ORIG": {Amount: 9000, Status: "PDNG"}}}
	for _, kind := range iso20022.RequestKinds() {
		rec := map[string]string{"Message": kind}
		for k, v := range base {
			rec[k] = v
		}
		for _, amt := range []string{"90.00", "900.00"} { // an acceptance and a rejection
			rec["Amount"] = amt
			req := iso20022.FromRecord(rec)
			out, err := sim.Reply(req)
			if err != nil {
				t.Fatalf("%s reply: %v", kind, err)
			}
			check(t, kind+" reply", out)
		}
	}
}

func check(t *testing.T, what string, raw []byte) {
	t.Helper()
	rep, err := Message(raw, "")
	if err != nil {
		t.Fatalf("%s: validate: %v", what, err)
	}
	if !rep.Schema {
		t.Errorf("%s: no schema bundled for %s", what, rep.Namespace)
		return
	}
	for _, f := range rep.Findings {
		if f.Severity == Error {
			t.Errorf("%s (%s): %s at %s: %s", what, rep.Kind, f.Rule, f.Path, f.Message)
		}
	}
	if t.Failed() && testing.Verbose() {
		t.Log(strings.TrimSpace(string(raw)))
	}
}

// TestDumpOutbound writes every outbound message and reply to samples/ as *-valid.xml when
// PROVA_DUMP_OUTBOUND=1, so gen.py can fold them into the libxml2 differential corpus.
func TestDumpOutbound(t *testing.T) {
	if os.Getenv("PROVA_DUMP_OUTBOUND") != "1" {
		t.Skip("set PROVA_DUMP_OUTBOUND=1 to regenerate the outbound samples")
	}
	sim := &iso20022.Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}, Entries: map[string]int{"ACC-EUR-1": 2},
		Mandates: map[string]iso20022.Mandate{"MNDT-1": {Account: "DE89370400440532013000"}}}
	for _, kind := range iso20022.RequestKinds() {
		rec := map[string]string{"Message": kind, "MsgId": "MSG-1", "EndToEndId": "E2E-1", "Amount": "90.00", "Currency": "EUR",
			"DebtorIBAN": "DE89370400440532013000", "CreditIBAN": "FR1420041010050500013M02606", "OrgnlEndToEndId": "E2E-ORIG",
			"Reason": "DUPL", "MandateId": "MNDT-1", "Account": "ACC-EUR-1", "Remittance": "Invoice 42", "UETR": "8a562c67-ca16-48ba-b074-65581be6f001"}
		req := iso20022.FromRecord(rec)
		raw, err := iso20022.Encode(req)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(kind, ".", "")
		write(t, "samples/outbound-"+name+"-valid.xml", raw)
		out, err := sim.Reply(req)
		if err != nil {
			t.Fatal(err)
		}
		write(t, "samples/outbound-"+name+"-reply-valid.xml", out)
	}
	for _, rk := range []string{iso20022.KindCamt052, iso20022.KindCamt054, "unknown-account"} {
		rec := map[string]string{"Message": iso20022.KindCamt060, "MsgId": "MSG-RPT", "Account": "ACC-EUR-1", "ReportKind": rk,
			"FromDate": "2026-10-01", "ToDate": "2026-10-20"}
		if rk == "unknown-account" {
			rec["Account"], rec["ReportKind"] = "ACC-NONE", iso20022.KindCamt053
		}
		out, err := sim.Reply(iso20022.FromRecord(rec))
		if err != nil {
			t.Fatal(err)
		}
		write(t, "samples/outbound-camt060-"+strings.ReplaceAll(rk, ".", "")+"-reply-valid.xml", out)
	}
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	// Fixed timestamps keep the samples stable across regenerations.
	s := regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ`).ReplaceAllString(string(b), "2026-10-20T09:30:00Z")
	s = regexp.MustCompile(`<Dt>\d{4}-\d\d-\d\d</Dt>`).ReplaceAllString(s, "<Dt>2026-10-20</Dt>")
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
