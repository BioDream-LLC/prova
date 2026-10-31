package engine

import (
	"strings"

	"github.com/biodream-llc/prova/internal/validate"
)

// The ISO 20022 catalogue family: for the latest version of every message in the catalogue, a
// schema-valid instance a gateway must acknowledge (admi.007), and one with a required element removed
// that it must reject (admi.002). It is the check a network makes before any business rule: SWIFT
// validates every MX message against its schema before delivering it, and a market infrastructure
// rejects one that fails.

// catalogueGroups puts the 31 business areas into the suites that run them.
var catalogueGroups = []struct {
	id, name, desc string
	areas          []string
}{
	{"catalogue-payments", "ISO 20022 catalogue: payments, cash and accounts",
		"Every payments-clearing (pacs), initiation (pain), cash-management (camt), remittance (remt), account-management (acmt), administration (admi) and business-application-header (head) message, in its latest version.",
		[]string{"pacs", "pain", "camt", "remt", "acmt", "admi", "head"}},
	{"catalogue-securities", "ISO 20022 catalogue: securities",
		"Every securities settlement (sese), management (semt), events (seev), trade (setr), clearing (secl) and collateral (colr) message, in its latest version.",
		[]string{"sese", "semt", "seev", "setr", "secl", "colr"}},
	{"catalogue-cards", "ISO 20022 catalogue: cards",
		"Every card message: acceptor to acquirer (caaa), acquirer to issuer (cain), sale to POI (casp), ATM (catp, caam), terminal (catm), network (canm), administration (caad), fees (cafc), files (cafm), fraud (cafr) and settlement reporting (casr).",
		[]string{"caaa", "caad", "caam", "cafc", "cafm", "cafr", "cain", "canm", "casp", "casr", "catm", "catp"}},
	{"catalogue-trade", "ISO 20022 catalogue: trade services",
		"Every trade services initiation (tsin), management (tsmt) and trade services (tsrv) message, in its latest version.",
		[]string{"tsin", "tsmt", "tsrv"}},
	{"catalogue-reference", "ISO 20022 catalogue: reference data, authorities and FX",
		"Every reference data (reda), authorities (auth) and foreign exchange trade (fxtr) message, in its latest version.",
		[]string{"reda", "auth", "fxtr"}},
}

func catalogueID(kind, variant string) string {
	m, _ := validate.LookupMessage(kind)
	return "mx-" + strings.ReplaceAll(m.Base, ".", "") + "-" + variant
}

// catalogueScenarios builds the family. The instances are generated when a scenario runs, not here.
func catalogueScenarios() []Scenario {
	var out []Scenario
	for _, m := range validate.Catalogue() {
		if !m.Latest {
			continue
		}
		label := m.Base + " " + m.Name
		out = append(out,
			Scenario{ID: catalogueID(m.Kind, "valid"), Name: label + " — a schema-valid message", Protocol: "iso20022", Message: m.Kind,
				Description: "Send a " + m.Kind + " (" + m.Name + ", " + strings.ToLower(validate.Areas[m.Area]) +
					") carrying only what its official schema requires. A gateway that validates against the schema must acknowledge it with an admi.007.",
				Request: map[string]string{"Variant": "minimal"}, Context: map[string]string{}, Expect: "acknowledge (admi.007)"},
			Scenario{ID: catalogueID(m.Kind, "broken"), Name: label + " — a required element missing", Protocol: "iso20022", Message: m.Kind,
				Description: "Send the same " + m.Kind + " with one required element removed, so it fails its official schema. The gateway must reject it with an admi.002 that names the error.",
				Request:     map[string]string{"Variant": "drop"}, Context: map[string]string{}, Expect: "reject (admi.002)"},
		)
	}
	return out
}

// catalogueSuites run the family by business-area group.
func catalogueSuites() []Suite {
	var out []Suite
	for _, g := range catalogueGroups {
		in := map[string]bool{}
		for _, a := range g.areas {
			in[a] = true
		}
		var ids []string
		for _, m := range validate.Catalogue() {
			if m.Latest && in[m.Area] {
				ids = append(ids, catalogueID(m.Kind, "valid"), catalogueID(m.Kind, "broken"))
			}
		}
		out = append(out, Suite{ID: g.id, Name: g.name, Target: "cbpr-uat", Scenarios: ids,
			Description: g.desc + " Each is sent valid, and then with a required element missing, to a gateway that must acknowledge the first and reject the second."})
	}
	return out
}

// isCatalogueScenario reports whether a scenario belongs to the catalogue family.
func isCatalogueScenario(id string) bool { return strings.HasPrefix(id, "mx-") }
