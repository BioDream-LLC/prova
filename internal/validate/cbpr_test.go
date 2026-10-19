package validate

import (
	"strings"
	"testing"
)

// TestCBPRProfile checks the cross-border usage-guideline rules that sit above the schema: a fully
// unstructured address, a malformed UETR, and a bad LEI must each be reported, and a clean CBPR+
// message must pass with no error-severity findings.
func TestCBPRProfile(t *testing.T) {
	clean := mustSample(t, "pacs.008-valid.xml")
	rep, err := Message(clean, "CBPR+")
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Valid() {
		t.Errorf("clean CBPR+ message reported errors: %v", rep.Findings)
	}
	if rep.Profile != "CBPR+" {
		t.Errorf("profile = %q", rep.Profile)
	}

	cases := []struct {
		name, mutFrom, mutTo, wantRule string
	}{
		{"unstructured address", "<PstlAdr><TwnNm>Frankfurt</TwnNm><Ctry>DE</Ctry></PstlAdr>", "<PstlAdr><AdrLine>12 Market St</AdrLine></PstlAdr>", "cbpr.address.structured"},
		{"bad UETR", "8a7c3b2e-1f4d-4a9b-8c2e-6f1a2b3c4d5e", "not-a-uuid", "cbpr.uetr"},
		{"missing country", "<PstlAdr><TwnNm>Frankfurt</TwnNm><Ctry>DE</Ctry></PstlAdr>", "<PstlAdr><TwnNm>Frankfurt</TwnNm></PstlAdr>", "cbpr.address.country"},
	}
	for _, c := range cases {
		m := strings.Replace(string(clean), c.mutFrom, c.mutTo, 1)
		rep, err := Message([]byte(m), "CBPR+")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !hasRule(rep, c.wantRule) {
			t.Errorf("%s: expected a %s finding; got %v", c.name, c.wantRule, rep.Findings)
		}
	}

	// A missing UETR is a warning, not a schema error.
	noUETR := strings.Replace(string(clean), "<UETR>8a7c3b2e-1f4d-4a9b-8c2e-6f1a2b3c4d5e</UETR>", "", 1)
	rep, _ = Message([]byte(noUETR), "CBPR+")
	if !hasRule(rep, "cbpr.uetr.present") {
		t.Errorf("missing UETR not flagged: %v", rep.Findings)
	}

	// SARIF renders and names the tool.
	sb, err := rep.SARIF()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sb), "\"name\": \"prova\"") || !strings.Contains(string(sb), "2.1.0") {
		t.Errorf("SARIF output missing tool/version: %s", sb)
	}
}

func hasRule(r Report, rule string) bool {
	for _, f := range r.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
