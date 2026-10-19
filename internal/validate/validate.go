package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// nsKind maps an ISO 20022 target namespace to its message short name.
func nsKind(ns string) string {
	const p = "urn:iso:std:iso:20022:tech:xsd:"
	if strings.HasPrefix(ns, p) {
		return strings.TrimPrefix(ns, p)
	}
	return ""
}

// Message validates one ISO 20022 message: its XML schema, then the usage-guideline profile. An
// empty profile applies the schema only; "CBPR+" also applies the cross-border profile rules.
func Message(raw []byte, profile string) (Report, error) {
	schemasOnce.Do(loadSchemas)
	if schemasErr != nil {
		return Report{}, schemasErr
	}
	doc, err := parseInstance(raw)
	if err != nil {
		return Report{}, fmt.Errorf("validate: parse message: %w", err)
	}
	ns := docNamespace(raw)
	rep := Report{Namespace: ns, Kind: nsKind(ns), Profile: profile}

	s := schemas[ns]
	if s == nil {
		rep.Findings = append(rep.Findings, Finding{
			Rule: "xsd.schema", Severity: Warning, Path: "/Document",
			Message: "no schema is bundled for namespace " + orNone(ns),
			Fix:     "prova validates pacs.008/009, pacs.002, pacs.004, pacs.028, camt.056/029/053/054 and pain.001/002",
		})
	} else {
		rep.Schema = true
		rep.Findings = append(rep.Findings, validateSchema(s, doc)...)
	}

	if strings.EqualFold(profile, "CBPR+") || strings.EqualFold(profile, "cbpr") {
		rep.Profile = "CBPR+"
		rep.Findings = append(rep.Findings, cbprFindings(doc, rep.Kind)...)
	}
	sortFindings(rep.Findings)
	return rep, nil
}

// docNamespace reads the xmlns of the element inside Document (the business root).
var reXmlns = regexp.MustCompile(`xmlns="([^"]+)"`)

func docNamespace(raw []byte) string {
	if m := reXmlns.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func sortFindings(fs []Finding) {
	sev := map[Severity]int{Error: 0, Warning: 1, Note: 2}
	sort.SliceStable(fs, func(i, j int) bool {
		if sev[fs[i].Severity] != sev[fs[j].Severity] {
			return sev[fs[i].Severity] < sev[fs[j].Severity]
		}
		return fs[i].Path < fs[j].Path
	})
}

// --- CBPR+ usage-guideline rules -----------------------------------------------------------
//
// These sit above the schema: a message can be schema-valid and still be refused by a correspondent
// under the SWIFT cross-border profile. prova checks the rules that bite hardest in the SR2025
// migration — structured addresses, a valid end-to-end UETR, and well-formed LEIs and BICs.

var (
	reUETR = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	reLEI  = regexp.MustCompile(`^[A-Z0-9]{18}[0-9]{2}$`)
	reBIC  = regexp.MustCompile(`^[A-Z]{6}[A-Z0-9]{2}([A-Z0-9]{3})?$`)
)

func cbprFindings(doc *node, kind string) []Finding {
	var out []Finding
	addr := strings.HasPrefix(kind, "pacs.008") || strings.HasPrefix(kind, "pacs.009") ||
		strings.HasPrefix(kind, "pain.001")

	// 1. Structured postal address: from November 2026 CBPR+ refuses a fully unstructured address.
	//    An address that carries AdrLine but neither TwnNm nor Ctry is unstructured.
	if addr {
		for _, pa := range findAll(doc, "PstlAdr") {
			p := pathOf(doc, pa)
			hasLine := child(pa, "AdrLine") != nil
			hasTown := child(pa, "TwnNm") != nil
			hasCtry := child(pa, "Ctry") != nil
			switch {
			case hasLine && !hasTown && !hasCtry:
				out = append(out, Finding{Rule: "cbpr.address.structured", Severity: Error, Path: p,
					Message: "postal address is fully unstructured (AdrLine only)",
					Fix:     "add structured TwnNm and Ctry (hybrid is permitted; fully unstructured is refused from November 2026)"})
			case !hasCtry:
				out = append(out, Finding{Rule: "cbpr.address.country", Severity: Warning, Path: p,
					Message: "postal address has no Ctry element",
					Fix:     "add a two-letter ISO country code in Ctry"})
			}
		}
	}

	// 2. UETR: the end-to-end unique transaction reference must be a UUIDv4.
	for _, u := range findAll(doc, "UETR") {
		if !reUETR.MatchString(strings.TrimSpace(u.text)) {
			out = append(out, Finding{Rule: "cbpr.uetr", Severity: Error, Path: pathOf(doc, u),
				Message: "UETR is not a valid UUIDv4",
				Fix:     "generate a version-4 UUID, e.g. 8a7c3b2e-1f4d-4a9b-8c2e-6f1a2b3c4d5e"})
		}
	}
	if addr && len(findAll(doc, "UETR")) == 0 {
		out = append(out, Finding{Rule: "cbpr.uetr.present", Severity: Warning, Path: "/Document",
			Message: "no UETR carried on the payment",
			Fix:     "CBPR+ requires an end-to-end UETR in PmtId/UETR"})
	}

	// 3. LEI, where present, must pass the ISO 17442 structure (18 alphanumerics + 2 check digits).
	for _, l := range findAll(doc, "LEI") {
		v := strings.TrimSpace(l.text)
		if !reLEI.MatchString(v) || !lei97(v) {
			out = append(out, Finding{Rule: "cbpr.lei", Severity: Error, Path: pathOf(doc, l),
				Message: "LEI " + v + " fails the ISO 17442 check",
				Fix:     "an LEI is 18 alphanumerics followed by two ISO 7064 mod-97-10 check digits"})
		}
	}

	// 4. Agent BICs must be well formed (8 or 11 characters).
	for _, b := range findAll(doc, "BICFI") {
		v := strings.TrimSpace(b.text)
		if !reBIC.MatchString(v) {
			out = append(out, Finding{Rule: "cbpr.bic", Severity: Error, Path: pathOf(doc, b),
				Message: "BIC " + v + " is not a valid 8- or 11-character BIC"})
		}
	}
	return out
}

// lei97 verifies the ISO 7064 mod 97-10 checksum over an LEI (letters -> two-digit values, remainder 1).
func lei97(lei string) bool {
	var sb strings.Builder
	for _, c := range lei {
		switch {
		case c >= '0' && c <= '9':
			sb.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			sb.WriteString(fmt.Sprintf("%d", int(c-'A')+10))
		default:
			return false
		}
	}
	rem := 0
	for _, c := range sb.String() {
		rem = (rem*10 + int(c-'0')) % 97
	}
	return rem == 1
}

// --- small tree helpers --------------------------------------------------------------------

func child(n *node, name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func findAll(root *node, name string) []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		if n.name == name {
			out = append(out, n)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// pathOf returns the /A/B/C path from root to target (first match).
func pathOf(root, target *node) string {
	var path []string
	var walk func(n *node) bool
	walk = func(n *node) bool {
		path = append(path, n.name)
		if n == target {
			return true
		}
		for _, c := range n.children {
			if walk(c) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if walk(root) {
		return "/" + strings.Join(path, "/")
	}
	return ""
}
