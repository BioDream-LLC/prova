package validate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func sample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("samples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func rules(r Report) map[string]bool {
	m := map[string]bool{}
	for _, f := range r.Findings {
		if f.Severity == Error {
			m[f.Rule] = true
		}
	}
	return m
}

// TestProfilesAcceptTheirSamples: a compliant message of each scheme passes its profile.
func TestProfilesAcceptTheirSamples(t *testing.T) {
	for file, profile := range map[string]string{
		"sepa-pacs008-valid.xml": "SEPA", "sepa-pain001-valid.xml": "SEPA", "sepa-inst-pacs008-valid.xml": "SEPA-Inst",
		"fednow-pacs008-valid.xml": "FedNow", "pacs.008-valid.xml": "CBPR+",
	} {
		rep, err := Message([]byte(sample(t, file)), profile)
		if err != nil {
			t.Fatal(err)
		}
		if !rep.Valid() {
			t.Errorf("%s under %s: %v", file, profile, rep.Findings)
		}
	}
}

// TestProfileRulesFire breaks a compliant message one rule at a time and checks the rule that should
// fire does.
func TestProfileRulesFire(t *testing.T) {
	sepa := sample(t, "sepa-pacs008-valid.xml")
	inst := sample(t, "sepa-inst-pacs008-valid.xml")
	fed := sample(t, "fednow-pacs008-valid.xml")
	cases := []struct {
		profile, rule, base, from, to string
	}{
		{"SEPA", "sepa.currency", sepa, `Ccy="EUR"`, `Ccy="USD"`},
		{"SEPA", "sepa.amount", sepa, `>250.00<`, `>1000000000.00<`},
		{"SEPA", "sepa.service-level", sepa, `<Cd>SEPA</Cd>`, `<Cd>NURG</Cd>`},
		{"SEPA", "sepa.charge-bearer", sepa, `<ChrgBr>SLEV</ChrgBr>`, `<ChrgBr>SHAR</ChrgBr>`},
		{"SEPA", "sepa.iban", sepa, `<IBAN>DE89370400440532013000</IBAN>`, `<Othr><Id>12345</Id></Othr>`},
		{"SEPA", "sepa.name.length", sepa, `<Nm>Muster GmbH</Nm>`, `<Nm>` + strings.Repeat("A", 71) + `</Nm>`},
		{"SEPA", "sepa.charset", sepa, `<Nm>Muster GmbH</Nm>`, `<Nm>Müller &amp; Söhne</Nm>`},
		{"SEPA", "sepa.remittance.length", sepa, `<Ustrd>Invoice 2026-0042</Ustrd>`, `<Ustrd>` + strings.Repeat("x", 141) + `</Ustrd>`},
		{"SEPA", "sepa.remittance.once", sepa, `<Ustrd>Invoice 2026-0042</Ustrd>`, `<Ustrd>a</Ustrd><Ustrd>b</Ustrd>`},
		{"SEPA", "sepa.identifier.slash", sepa, `<EndToEndId>SEPA-E2E-1</EndToEndId>`, `<EndToEndId>/SEPA-E2E-1</EndToEndId>`},
		{"SEPA", "sepa.identifier.slash", sepa, `<EndToEndId>SEPA-E2E-1</EndToEndId>`, `<EndToEndId>SEPA//E2E</EndToEndId>`},
		{"SEPA", "sepa.address.structured", sepa, `<TwnNm>Frankfurt</TwnNm>`, `<AdrLine>Hauptstrasse 1</AdrLine>`},
		{"SEPA-Inst", "sepa.inst.local-instrument", inst, `<Cd>INST</Cd>`, `<Cd>CORE</Cd>`},
		{"FedNow", "fednow.currency", fed, `Ccy="USD"`, `Ccy="EUR"`},
		{"FedNow", "fednow.limit", fed, `>2500.00<`, `>1000000.01<`},
		{"FedNow", "fednow.charge-bearer", fed, `<ChrgBr>SLEV</ChrgBr>`, `<ChrgBr>SHAR</ChrgBr>`},
		{"FedNow", "fednow.agent.rtn.check", fed, `<MmbId>011000015</MmbId>`, `<MmbId>011000016</MmbId>`},
		{"FedNow", "fednow.agent.usaba", fed, `<Cd>USABA</Cd>`, `<Cd>GBDSC</Cd>`},
		{"FedNow", "fednow.single", fed, `<NbOfTxs>1</NbOfTxs>`, `<NbOfTxs>2</NbOfTxs>`},
		{"FedNow", "fednow.future", fed, `<CreDtTm>2026-10-20T09:30:00Z</CreDtTm>`, `<CreDtTm>2999-01-01T00:00:00Z</CreDtTm>`},
		{"FedNow", "fednow.name", fed, `<Nm>Acme Corp</Nm>`, ``},
	}
	for _, c := range cases {
		if !strings.Contains(c.base, c.from) {
			t.Fatalf("%s: sample lacks %q", c.rule, c.from)
		}
		rep, err := Message([]byte(strings.Replace(c.base, c.from, c.to, 1)), c.profile)
		if err != nil {
			t.Fatal(err)
		}
		if !rules(rep)[c.rule] {
			t.Errorf("%s did not fire: %v", c.rule, rep.Findings)
		}
	}
	if _, err := Message([]byte(sepa), "NOPE"); err == nil {
		t.Error("an unknown profile was accepted")
	}
	if !abaValid("021000021") || abaValid("021000022") {
		t.Error("ABA check digit")
	}
}

// TestUsageGuidelineSchema validates against a restricted copy of the pacs.008 schema, as a market's
// usage guideline (or a MyStandards export) restricts the base: here charges may only be SLEV and the
// UETR is required.
func TestUsageGuidelineSchema(t *testing.T) {
	base, err := os.ReadFile("xsd/pacs.008.001.08.xsd")
	if err != nil {
		t.Fatal(err)
	}
	ug := strings.Replace(string(base), `<xs:enumeration value="DEBT"/>`, ``, 1)
	ug = strings.Replace(ug, `<xs:enumeration value="CRED"/>`, ``, 1)
	ug = strings.Replace(ug, `<xs:enumeration value="SHAR"/>`, ``, 1)
	ug = strings.Replace(ug, `<xs:element maxOccurs="1" minOccurs="0" name="UETR" type="UUIDv4Identifier"/>`, `<xs:element name="UETR" type="UUIDv4Identifier"/>`, 1)
	// ChrgBr SHAR and no UETR: schema-valid, guideline-invalid.
	msg := regexp.MustCompile(`\s*<UETR>[^<]*</UETR>`).ReplaceAllString(sample(t, "pacs.008-valid.xml"), "")
	rep, err := MessageWith([]byte(msg), "", []byte(ug))
	if err != nil {
		t.Fatal(err)
	}
	r := rules(rep)
	if !rep.Guideline || !r["guideline.enumeration"] || !r["guideline.minOccurs"] {
		t.Errorf("guideline findings missing: %v", rep.Findings)
	}
	for k := range r {
		if strings.HasPrefix(k, "xsd.") {
			t.Errorf("the base schema should accept the sample: %v", rep.Findings)
		}
	}
	sepa := sample(t, "sepa-pacs008-valid.xml") // SLEV and a UETR: passes the guideline too
	if rep, _ := MessageWith([]byte(sepa), "", []byte(ug)); !rep.Valid() {
		t.Errorf("a compliant message fails the guideline: %v", rep.Findings)
	}
	other, _ := os.ReadFile("xsd/pacs.009.001.08.xsd")
	if rep, _ := MessageWith([]byte(msg), "", other); !rules(rep)["guideline.namespace"] {
		t.Error("a guideline for another message was applied")
	}
}
