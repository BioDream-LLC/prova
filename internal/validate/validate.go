package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// nsKind maps an ISO 20022 target namespace to its message version.
func nsKind(ns string) string {
	const p = "urn:iso:std:iso:20022:tech:xsd:"
	if k := KindForNamespace(ns); k != "" {
		return k
	}
	if strings.HasPrefix(ns, p) {
		return strings.TrimPrefix(ns, p)
	}
	return ""
}

// Message validates one ISO 20022 message: its XML schema, then the usage-guideline profile. An
// empty profile applies the schema only; see Profiles for the others.
func Message(raw []byte, profile string) (Report, error) { return MessageWith(raw, profile, nil) }

// maxGuideline caps an uploaded usage-guideline schema.
const maxGuideline = 8 << 20

// MessageWith validates a message against the bundled schema, a profile and, when guideline is given,
// a usage-guideline schema: the restricted XSD a market infrastructure publishes or MyStandards exports
// for its usage guideline. Findings from the guideline schema carry rule ids "guideline.*".
func MessageWith(raw []byte, profile string, guideline []byte) (Report, error) {
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
	if s == nil && rep.Kind != "" {
		if cs, err := schemaForKind(rep.Kind); err == nil {
			s = cs
		}
	}
	switch {
	case s != nil && s.ns != ns:
		rep.Schema = true
		rep.Findings = append(rep.Findings, Finding{Rule: "xsd.namespace", Severity: Error, Path: "/Document",
			Message: "the namespace is " + orNone(ns) + ", but " + rep.Kind + "'s schema is for " + s.ns, Fix: "use the namespace " + s.ns})
	case s == nil:
		rep.Findings = append(rep.Findings, Finding{
			Rule: "xsd.schema", Severity: Warning, Path: "/Document",
			Message: "no ISO 20022 schema exists for namespace " + orNone(ns),
			Fix:     "the namespace must be urn:iso:std:iso:20022:tech:xsd: followed by a message version in the catalogue, such as pacs.008.001.08",
		})
	default:
		rep.Schema = true
		rep.Findings = append(rep.Findings, validateSchema(s, doc)...)
	}

	if len(guideline) > 0 {
		if len(guideline) > maxGuideline {
			return Report{}, fmt.Errorf("validate: the usage-guideline schema is larger than %d MB", maxGuideline>>20)
		}
		g, err := parseSchema(guideline)
		if err != nil {
			return Report{}, fmt.Errorf("validate: usage-guideline schema: %w", err)
		}
		rep.Guideline = true
		if g.ns != ns {
			rep.Findings = append(rep.Findings, Finding{Rule: "guideline.namespace", Severity: Error, Path: "/Document",
				Message: "the usage-guideline schema is for " + orNone(g.ns) + ", the message is " + orNone(ns)})
		} else {
			seen := map[string]bool{}
			for _, f := range rep.Findings {
				seen[f.Path+"|"+f.Message] = true
			}
			for _, f := range validateSchema(g, doc) {
				if seen[f.Path+"|"+f.Message] {
					continue // the base schema already reports it
				}
				f.Rule = "guideline." + strings.TrimPrefix(f.Rule, "xsd.")
				rep.Findings = append(rep.Findings, f)
			}
		}
	}

	name, fs, ok := profileFindings(doc, rep.Kind, profile)
	if !ok {
		return Report{}, fmt.Errorf("validate: unknown profile %q (one of CBPR+, SEPA, SEPA-Inst, FedNow)", profile)
	}
	rep.Profile = name
	rep.Findings = append(rep.Findings, fs...)
	if rep.Findings == nil {
		rep.Findings = []Finding{} // a clean message has an empty list, not none
	}
	sortFindings(rep.Findings)
	return rep, nil
}

// bundledKinds lists the curated schemas (the messages prova speaks natively) from their file names.
func bundledKinds() []string {
	ents, _ := schemaFS.ReadDir("xsd")
	var out []string
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".xsd"))
	}
	return out
}

// Bundled lists the message versions prova speaks natively, with a curated schema, sorted. Every
// other message in the ISO 20022 catalogue validates too; see Catalogue.
func Bundled() []string {
	schemasOnce.Do(loadSchemas)
	out := make([]string, 0, len(schemas))
	for ns := range schemas {
		out = append(out, nsKind(ns))
	}
	sort.Strings(out)
	return out
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
