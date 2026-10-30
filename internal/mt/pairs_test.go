package mt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/validate"
)

// samples are one MT message per pair, as a bank would send them.
var samples = func() map[string]string {
	m := map[string]string{}
	for k, v := range Samples() {
		m[k] = v
	}
	m["MT103"] = sampleMT103
	return m
}()

func schemaValid(t *testing.T, what, xml string) {
	t.Helper()
	rep, err := validate.Message([]byte(xml), "")
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !rep.Schema || !rep.Valid() {
		t.Errorf("%s: not schema-valid: %v\n%s", what, rep.Findings, xml)
	}
}

// TestEveryPairIsSchemaValid transforms each sample MT to MX, checks the MX against the official schema,
// transforms it back, and checks the money and references survived both ways.
func TestEveryPairIsSchemaValid(t *testing.T) {
	for _, p := range Pairs {
		src, ok := samples[p.MT]
		if !ok {
			t.Fatalf("no sample for %s", p.MT)
		}
		mx, err := ToMX(src)
		if err != nil {
			t.Fatalf("%s: %v", p.MT, err)
		}
		if mx.From != p.MT || mx.To != p.MX {
			t.Errorf("%s: transformed as %s -> %s, want %s -> %s", p.MT, mx.From, mx.To, p.MT, p.MX)
		}
		schemaValid(t, p.MT+" -> "+p.MX, mx.Output)
		back, err := ToMT([]byte(mx.Output))
		if err != nil {
			t.Fatalf("%s back: %v", p.MX, err)
		}
		if back.To != p.MT {
			t.Errorf("%s came back as %s, want %s", p.MX, back.To, p.MT)
		}
		f1, _ := parseBlock4(src)
		f2, err := parseBlock4(back.Output)
		if err != nil {
			t.Fatalf("%s: output is not an MT message: %v\n%s", p.MT, err, back.Output)
		}
		for _, tag := range []string{"20", "21", "25", "32A", "62F", "79"} {
			want := strings.TrimSpace(f1[tag])
			if tag == "79" {
				want = codeIn(want)
				f2[tag] = codeIn(f2[tag])
			}
			if want != "" && strings.TrimSpace(f2[tag]) != want {
				t.Errorf("%s round trip: :%s: is %q, was %q", p.MT, tag, f2[tag], want)
			}
		}
		if p.MT == "MT103 RETN" {
			if r1, m1 := retnInfo(f1["72"]); true {
				r2, m2 := retnInfo(f2["72"])
				if r1 != r2 || m1 != m2 {
					t.Errorf("RETN round trip: %s/%s -> %s/%s", r1, m1, r2, m2)
				}
			}
		}
	}
}

func TestMX2MTDetails(t *testing.T) {
	mx, _ := ToMX(samples["MT940"])
	st, err := iso20022.DecodeStatus([]byte(mx.Output))
	if err != nil {
		t.Fatal(err)
	}
	if st.OpeningBalance != "52.00" || st.ClosingBalance != "141.50" || len(st.EntryList) != 2 ||
		st.EntryList[1].CreditDebit != "DBIT" || st.EntryList[1].Amount != "0.50" || st.EntryList[0].Ref != "E2E-1" {
		t.Errorf("MT940 -> camt.053: %+v", st)
	}
	ret, _ := ToMX(samples["MT103 RETN"])
	r, err := iso20022.ParseRequest([]byte(ret.Output))
	if err != nil || r.Reason != "AC03" || r.OrgnlEndToEndId != "E2E-ORIG-SETTLED" || r.Amount != "90.00" {
		t.Errorf("MT103 RETN -> pacs.004: %+v %v", r, err)
	}
	long, _ := iso20022.Encode(iso20022.FromRecord(map[string]string{"EndToEndId": "A-VERY-LONG-END-TO-END-ID", "Amount": "1.00"}))
	res, _ := ToMT(long)
	if len(res.Notes) == 0 || !strings.Contains(res.Output, ":20:A-VERY-LONG-END") {
		t.Errorf("a long reference is cut to 16 with a note: %v", res.Notes)
	}
	if _, err := ToMT([]byte("<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:pacs.028.001.03\"><FIToFIPmtStsReq/></Document>")); err == nil {
		t.Error("pacs.028 has no MT pair but was transformed")
	}
}

// TestMT940AgreesWithParser has an independent MT940 parser (the BSD-licensed mt-940 Python library)
// read prova's MT940 output. Set PROVA_TEST_MT940 to a python that has mt-940 installed.
func TestMT940AgreesWithParser(t *testing.T) {
	py := os.Getenv("PROVA_TEST_MT940")
	if py == "" {
		t.Skip("set PROVA_TEST_MT940=/path/to/python with mt-940 installed")
	}
	stmt := iso20022.Status{Kind: iso20022.KindCamt053, MsgId: "STMT-42", Account: "DE89370400440532013000", Currency: "EUR",
		OpeningBalance: "1000.00", ClosingBalance: "1234.56", EntryList: []iso20022.Entry{
			{Amount: "300.00", Currency: "EUR", CreditDebit: "CRDT", Ref: "INV-1", ValueDate: "2026-10-20"},
			{Amount: "65.44", Currency: "EUR", CreditDebit: "DBIT", Ref: "FEE-2", ValueDate: "2026-10-21"},
		}}
	raw, _ := iso20022.EncodeStatus(stmt)
	res, err := ToMT(raw)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "stmt.txt")
	if err := os.WriteFile(path, []byte(res.Output), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `import sys, mt940
t = mt940.models.Transactions(); t.parse(open(sys.argv[1]).read())
d = t.data
print(d["final_opening_balance"].amount.amount, d["final_closing_balance"].amount.amount, d["account_identification"])
for x in t: print(x.data["amount"].amount, x.data["date"], x.data.get("customer_reference"))`
	out, err := exec.Command(py, "-c", script, path).CombinedOutput()
	if err != nil {
		t.Fatalf("mt-940 could not read prova's MT940: %v\n%s\n%s", err, out, res.Output)
	}
	want := "1000.00 1234.56 DE89370400440532013000\n300.00 2026-10-20 INV-1\n-65.44 2026-10-21 FEE-2\n"
	if string(out) != want {
		t.Errorf("mt-940 read\n%s\nwant\n%s\nfrom\n%s", out, want, res.Output)
	}
}
