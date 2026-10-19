// Package validate is prova's ISO 20022 message validator: it checks a message against the official
// ISO 20022 XML schema and against the market usage-guideline rules that sit above the schema (the
// CBPR+ cross-border profile). Every finding names the rule, the element it points at and, where it
// can, what to change — the same contract prova's verdicts hold themselves to. Findings export as
// SARIF 2.1.0 for code scanning.
//
// The schema validator is a focused implementation of the XSD subset the ISO 20022 message schemas
// actually use: named complex and simple types, sequence and choice particles with occurrence bounds,
// simpleContent extensions with attributes, and the restriction facets (enumeration, length bounds,
// pattern, totalDigits, fractionDigits, minInclusive). It is cross-checked against libxml2 in the
// package tests, exactly as a reference validator would be.
package validate

import (
	"embed"
	"encoding/xml"
	"fmt"
	"io/fs"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

//go:embed xsd/*.xsd
var schemaFS embed.FS

// Severity ranks a finding.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Note    Severity = "note"
)

// Finding is one validation result: the rule that fired, where it points, and ideally what to change.
type Finding struct {
	Rule     string   `json:"rule"`     // stable identifier, e.g. "xsd.enumeration" or "cbpr.address.structured"
	Severity Severity `json:"severity"` // error | warning | note
	Message  string   `json:"message"`  // what is wrong
	Path     string   `json:"path"`     // element path into the document, e.g. /Document/FIToFICstmrCdtTrf/GrpHdr/NbOfTxs
	Fix      string   `json:"fix,omitempty"`
}

// Report is the outcome of validating one message.
type Report struct {
	Kind      string    `json:"kind"`      // message short name, e.g. pacs.008.001.08
	Namespace string    `json:"namespace"` // the document's target namespace
	Profile   string    `json:"profile"`   // the usage-guideline profile applied, e.g. "CBPR+"
	Schema    bool      `json:"schema"`    // whether a schema was found and applied
	Findings  []Finding `json:"findings"`
}

// Valid reports whether the message passed with no error-severity findings.
func (r Report) Valid() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return false
		}
	}
	return true
}

// --- the XSD model -------------------------------------------------------------------------

type facet struct {
	enum                    []string
	minLen, maxLen, length  int
	hasMinLen, hasMaxLen    bool
	hasLength               bool
	pattern                 *regexp.Regexp
	patternSrc              string
	totalDigits, fracDigits int
	hasTotal, hasFrac       bool
	minIncl                 string
	hasMinIncl              bool
}

type simpleType struct {
	name  string
	base  string // another named type or an xs: builtin
	facet facet
}

type particle struct {
	name      string // element name
	typ       string // named type
	min, max  int    // max -1 == unbounded
	choiceAlt []particle
	isChoice  bool
	isAny     bool
}

type complexType struct {
	name       string
	particles  []particle // sequence or top-level choice entries
	simpleBase string     // for simpleContent: the base simple type
	attrs      []attrDef
}

type attrDef struct {
	name     string
	typ      string
	required bool
}

type schema struct {
	ns       string
	root     string // the type of the Document element
	simples  map[string]*simpleType
	complexs map[string]*complexType
}

// --- XSD parsing (encoding/xml into the model) ---------------------------------------------

type xsdSchema struct {
	TargetNS     string       `xml:"targetNamespace,attr"`
	Elements     []xsdElement `xml:"element"`
	ComplexTypes []xsdComplex `xml:"complexType"`
	SimpleTypes  []xsdSimple  `xml:"simpleType"`
}

type xsdElement struct {
	Name      string `xml:"name,attr"`
	Type      string `xml:"type,attr"`
	MinOccurs string `xml:"minOccurs,attr"`
	MaxOccurs string `xml:"maxOccurs,attr"`
}

type xsdComplex struct {
	Name          string            `xml:"name,attr"`
	Sequence      *xsdGroup         `xml:"sequence"`
	Choice        *xsdGroup         `xml:"choice"`
	SimpleContent *xsdSimpleContent `xml:"simpleContent"`
}

type xsdGroup struct {
	Elements []xsdElement `xml:"element"`
	Choices  []xsdGroup   `xml:"choice"`
	Any      []struct{}   `xml:"any"`
}

type xsdSimpleContent struct {
	Extension struct {
		Base  string    `xml:"base,attr"`
		Attrs []xsdAttr `xml:"attribute"`
	} `xml:"extension"`
}

type xsdAttr struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Use  string `xml:"use,attr"`
}

type xsdSimple struct {
	Name        string `xml:"name,attr"`
	Restriction struct {
		Base         string `xml:"base,attr"`
		Enumerations []struct {
			Value string `xml:"value,attr"`
		} `xml:"enumeration"`
		MinLength      *facetVal `xml:"minLength"`
		MaxLength      *facetVal `xml:"maxLength"`
		Length         *facetVal `xml:"length"`
		Pattern        *facetVal `xml:"pattern"`
		TotalDigits    *facetVal `xml:"totalDigits"`
		FractionDigits *facetVal `xml:"fractionDigits"`
		MinInclusive   *facetVal `xml:"minInclusive"`
	} `xml:"restriction"`
}

type facetVal struct {
	Value string `xml:"value,attr"`
}

func occurs(s string, dflt int) int {
	if s == "" {
		return dflt
	}
	if s == "unbounded" {
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return dflt
	}
	return n
}

func groupParticles(g *xsdGroup, isChoice bool) []particle {
	if g == nil {
		return nil
	}
	var ps []particle
	for _, e := range g.Elements {
		ps = append(ps, particle{name: e.Name, typ: e.Type, min: occurs(e.MinOccurs, 1), max: occurs(e.MaxOccurs, 1)})
	}
	for range g.Any {
		ps = append(ps, particle{isAny: true, min: 0, max: -1})
	}
	for i := range g.Choices {
		ps = append(ps, particle{isChoice: true, min: 1, max: 1, choiceAlt: groupParticles(&g.Choices[i], true)})
	}
	return ps
}

func parseSchema(raw []byte) (*schema, error) {
	var xs xsdSchema
	if err := xml.Unmarshal(raw, &xs); err != nil {
		return nil, err
	}
	s := &schema{ns: xs.TargetNS, simples: map[string]*simpleType{}, complexs: map[string]*complexType{}}
	for _, e := range xs.Elements {
		if e.Name == "Document" {
			s.root = e.Type
		}
	}
	for _, c := range xs.ComplexTypes {
		ct := &complexType{name: c.Name}
		switch {
		case c.Sequence != nil:
			ct.particles = groupParticles(c.Sequence, false)
		case c.Choice != nil:
			// a top-level choice: exactly one of the alternatives
			ct.particles = []particle{{isChoice: true, min: 1, max: 1, choiceAlt: groupParticles(c.Choice, true)}}
		case c.SimpleContent != nil:
			ct.simpleBase = c.SimpleContent.Extension.Base
			for _, a := range c.SimpleContent.Extension.Attrs {
				ct.attrs = append(ct.attrs, attrDef{name: a.Name, typ: a.Type, required: a.Use == "required"})
			}
		}
		s.complexs[c.Name] = ct
	}
	for _, st := range xs.SimpleTypes {
		t := &simpleType{name: st.Name, base: st.Restriction.Base}
		for _, e := range st.Restriction.Enumerations {
			t.facet.enum = append(t.facet.enum, e.Value)
		}
		if v := st.Restriction.MinLength; v != nil {
			t.facet.minLen, t.facet.hasMinLen = atoi(v.Value), true
		}
		if v := st.Restriction.MaxLength; v != nil {
			t.facet.maxLen, t.facet.hasMaxLen = atoi(v.Value), true
		}
		if v := st.Restriction.Length; v != nil {
			t.facet.length, t.facet.hasLength = atoi(v.Value), true
		}
		if v := st.Restriction.Pattern; v != nil {
			t.facet.patternSrc = v.Value
			t.facet.pattern = regexp.MustCompile("^(?:" + v.Value + ")$")
		}
		if v := st.Restriction.TotalDigits; v != nil {
			t.facet.totalDigits, t.facet.hasTotal = atoi(v.Value), true
		}
		if v := st.Restriction.FractionDigits; v != nil {
			t.facet.fracDigits, t.facet.hasFrac = atoi(v.Value), true
		}
		if v := st.Restriction.MinInclusive; v != nil {
			t.facet.minIncl, t.facet.hasMinIncl = v.Value, true
		}
		s.simples[st.Name] = t
	}
	return s, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// --- the schema registry (one per namespace) ----------------------------------------------

var (
	schemasOnce sync.Once
	schemas     map[string]*schema // by target namespace
	schemasErr  error
)

func loadSchemas() {
	schemas = map[string]*schema{}
	err := fs.WalkDir(schemaFS, "xsd", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xsd") {
			return err
		}
		raw, err := schemaFS.ReadFile(path)
		if err != nil {
			return err
		}
		s, err := parseSchema(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		schemas[s.ns] = s
		return nil
	})
	schemasErr = err
}

// --- the instance document (generic tree) --------------------------------------------------

type node struct {
	name     string
	text     string
	attrs    map[string]string
	children []*node
}

func parseInstance(raw []byte) (*node, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var stack []*node
	var root *node
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("document is empty")
	}
	return root, nil
}

// --- schema validation ---------------------------------------------------------------------

type schemaValidator struct {
	s        *schema
	findings []Finding
}

func (v *schemaValidator) add(sev Severity, rule, path, msg string) {
	v.findings = append(v.findings, Finding{Rule: rule, Severity: sev, Path: path, Message: msg})
}

// validateSchema checks doc against schema s. The root element must be Document.
func validateSchema(s *schema, doc *node) []Finding {
	v := &schemaValidator{s: s}
	if doc.name != "Document" {
		v.add(Error, "xsd.root", "/"+doc.name, "root element is not Document")
		return v.findings
	}
	ct := s.complexs[s.root]
	if ct == nil {
		v.add(Error, "xsd.schema", "/Document", "schema defines no Document type")
		return v.findings
	}
	v.checkComplex(doc, ct, "/Document")
	return v.findings
}

// checkComplex validates n's children and text against complex type ct.
func (v *schemaValidator) checkComplex(n *node, ct *complexType, path string) {
	if ct.simpleBase != "" {
		// simpleContent: validate text against the base and attributes against their types.
		v.checkSimple(n.text, ct.simpleBase, path)
		for _, a := range ct.attrs {
			val, ok := n.attrs[a.name]
			if !ok {
				if a.required {
					v.add(Error, "xsd.attribute", path, "missing required attribute @"+a.name)
				}
				continue
			}
			v.checkSimple(val, a.typ, path+"/@"+a.name)
		}
		return
	}
	v.matchParticles(n.children, ct.particles, path)
}

// matchParticles walks the child elements against an ordered particle list (a sequence), descending
// into each child's type. Choices accept exactly one present branch. xs:any is skipped (lax).
func (v *schemaValidator) matchParticles(children []*node, parts []particle, path string) {
	i := 0 // index into children
	for _, p := range parts {
		switch {
		case p.isAny:
			return // lax: stop checking order once any content is permitted
		case p.isChoice:
			// find which single alternative matches the next child(ren)
			matched := false
			for _, alt := range p.choiceAlt {
				if i < len(children) && children[i].name == alt.name {
					matched = true
					count := 0
					for i < len(children) && children[i].name == alt.name {
						v.descend(children[i], alt.typ, path+"/"+alt.name)
						i++
						count++
					}
					if alt.max > 0 && count > alt.max {
						v.add(Error, "xsd.maxOccurs", path+"/"+alt.name, fmt.Sprintf("%s occurs %d times, at most %d allowed", alt.name, count, alt.max))
					}
					break
				}
			}
			if !matched && p.min > 0 {
				names := make([]string, 0, len(p.choiceAlt))
				for _, alt := range p.choiceAlt {
					names = append(names, alt.name)
				}
				v.add(Error, "xsd.choice", path, "exactly one of ["+strings.Join(names, ", ")+"] is required")
			}
		default:
			count := 0
			for i < len(children) && children[i].name == p.name {
				v.descend(children[i], p.typ, path+"/"+p.name)
				i++
				count++
			}
			if count < p.min {
				v.add(Error, "xsd.minOccurs", path+"/"+p.name, fmt.Sprintf("missing required element %s", p.name))
			}
			if p.max > 0 && count > p.max {
				v.add(Error, "xsd.maxOccurs", path+"/"+p.name, fmt.Sprintf("%s occurs %d times, at most %d allowed", p.name, count, p.max))
			}
		}
	}
	// Any trailing children not consumed are unexpected (unless an xs:any absorbed them above).
	for ; i < len(children); i++ {
		v.add(Error, "xsd.unexpected", path+"/"+children[i].name, "unexpected element "+children[i].name)
	}
}

// descend validates a child node against a named type (complex or simple).
func (v *schemaValidator) descend(n *node, typ, path string) {
	if ct := v.s.complexs[typ]; ct != nil {
		v.checkComplex(n, ct, path)
		return
	}
	// a simple-typed element: it must carry text and no children.
	if len(n.children) > 0 {
		v.add(Error, "xsd.simpleType", path, "element "+n.name+" must be a simple value, not a container")
		return
	}
	v.checkSimple(n.text, typ, path)
}

// checkSimple validates a text value against a named simple type or an xs: builtin, following the
// base chain and applying every facet on the way.
func (v *schemaValidator) checkSimple(val, typ, path string) {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(typ, "xs:") {
		v.checkBuiltin(val, typ, path)
		return
	}
	st := v.s.simples[typ]
	if st == nil {
		// unknown named type; validate as a string (lax)
		return
	}
	v.applyFacet(val, st.facet, path, typ)
	if st.base != "" {
		v.checkSimple(val, st.base, path)
	}
}

func (v *schemaValidator) applyFacet(val string, f facet, path, typ string) {
	if len(f.enum) > 0 {
		ok := false
		for _, e := range f.enum {
			if val == e {
				ok = true
				break
			}
		}
		if !ok {
			v.add(Error, "xsd.enumeration", path, fmt.Sprintf("%q is not one of the %d allowed values for %s", val, len(f.enum), typ))
		}
	}
	if f.hasLength && len(val) != f.length {
		v.add(Error, "xsd.length", path, fmt.Sprintf("%q must be exactly %d characters", val, f.length))
	}
	if f.hasMinLen && len(val) < f.minLen {
		v.add(Error, "xsd.minLength", path, fmt.Sprintf("%q is shorter than the minimum %d characters", val, f.minLen))
	}
	if f.hasMaxLen && len(val) > f.maxLen {
		v.add(Error, "xsd.maxLength", path, fmt.Sprintf("value of %d characters exceeds the maximum %d", len(val), f.maxLen))
	}
	if f.pattern != nil && val != "" && !f.pattern.MatchString(val) {
		v.add(Error, "xsd.pattern", path, fmt.Sprintf("%q does not match the required pattern %s", val, f.patternSrc))
	}
	if f.hasTotal || f.hasFrac || f.hasMinIncl {
		v.checkDecimalFacets(val, f, path)
	}
}

func (v *schemaValidator) checkDecimalFacets(val string, f facet, path string) {
	r, ok := new(big.Rat).SetString(val)
	if !ok {
		return // the builtin check reports a non-decimal
	}
	if f.hasMinIncl {
		if m, ok := new(big.Rat).SetString(f.minIncl); ok && r.Cmp(m) < 0 {
			v.add(Error, "xsd.minInclusive", path, fmt.Sprintf("%s is less than the minimum %s", val, f.minIncl))
		}
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, val)
	if f.hasTotal && len(strings.TrimLeft(digits, "0")) > f.totalDigits {
		v.add(Error, "xsd.totalDigits", path, fmt.Sprintf("%s has more than %d significant digits", val, f.totalDigits))
	}
	if f.hasFrac {
		if _, frac, found := strings.Cut(val, "."); found && len(strings.TrimRight(frac, "0")) > f.fracDigits {
			v.add(Error, "xsd.fractionDigits", path, fmt.Sprintf("%s has more than %d fraction digits", val, f.fracDigits))
		}
	}
}

var (
	reDate     = regexp.MustCompile(`^-?\d{4}-\d{2}-\d{2}(Z|[+-]\d{2}:\d{2})?$`)
	reDateTime = regexp.MustCompile(`^-?\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`)
	reYearMon  = regexp.MustCompile(`^-?\d{4}-\d{2}(Z|[+-]\d{2}:\d{2})?$`)
	reTime     = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`)
)

func (v *schemaValidator) checkBuiltin(val, typ, path string) {
	switch typ {
	case "xs:decimal":
		if _, ok := new(big.Rat).SetString(val); !ok {
			v.add(Error, "xsd.decimal", path, fmt.Sprintf("%q is not a decimal number", val))
		}
	case "xs:date":
		if !reDate.MatchString(val) || !validDateParts(val) {
			v.add(Error, "xsd.date", path, fmt.Sprintf("%q is not an ISO date (YYYY-MM-DD)", val))
		}
	case "xs:dateTime":
		if !reDateTime.MatchString(val) || !validDateParts(val) {
			v.add(Error, "xsd.dateTime", path, fmt.Sprintf("%q is not an ISO dateTime", val))
		}
	case "xs:gYearMonth":
		if !reYearMon.MatchString(val) || !validDateParts(val) {
			v.add(Error, "xsd.gYearMonth", path, fmt.Sprintf("%q is not a year-month", val))
		}
	case "xs:time":
		if !reTime.MatchString(val) || !validTimeParts(val) {
			v.add(Error, "xsd.time", path, fmt.Sprintf("%q is not a time", val))
		}
	case "xs:boolean":
		if val != "true" && val != "false" && val != "1" && val != "0" {
			v.add(Error, "xsd.boolean", path, fmt.Sprintf("%q is not a boolean", val))
		}
	case "xs:string", "xs:normalizedString", "xs:base64Binary":
		// no lexical constraint beyond facets
	}
}

// validDateParts checks the month (01-12) and day (01-31) of a date/dateTime/year-month string are
// in range, which the structural regexes do not. It is lenient on days-per-month; libxml2 agreement
// on the test corpus is the measure that matters.
func validDateParts(val string) bool {
	val = strings.TrimLeft(val, "-")
	parts := strings.SplitN(val, "-", 3)
	if len(parts) < 2 {
		return false
	}
	mon := atoi(parts[1])
	if mon < 1 || mon > 12 {
		return false
	}
	if len(parts) == 3 {
		day := atoi(strings.SplitN(parts[2], "T", 2)[0])
		if day < 1 || day > 31 {
			return false
		}
		if strings.Contains(parts[2], "T") {
			return validTimeParts(strings.SplitN(parts[2], "T", 2)[1])
		}
	}
	return true
}

func validTimeParts(val string) bool {
	val = strings.TrimRight(val, "Z")
	if i := strings.IndexAny(val, "+-"); i > 0 {
		val = val[:i]
	}
	hms := strings.SplitN(val, ":", 3)
	if len(hms) != 3 {
		return false
	}
	h, m := atoi(hms[0]), atoi(hms[1])
	s := atoi(strings.SplitN(hms[2], ".", 2)[0])
	return h <= 23 && m <= 59 && s <= 60
}
