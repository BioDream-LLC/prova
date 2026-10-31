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
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io/fs"
	"math/big"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
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
	Kind      string `json:"kind"`                // message short name, e.g. pacs.008.001.08
	Namespace string `json:"namespace"`           // the document's target namespace
	Profile   string `json:"profile"`             // the usage-guideline profile applied, e.g. "CBPR+"
	Schema    bool   `json:"schema"`              // whether a schema was found and applied
	Guideline bool   `json:"guideline,omitempty"` // whether a usage-guideline schema was applied too
	// GuidelineID names the library guideline applied, when it came from the library.
	GuidelineID string    `json:"guidelineId,omitempty"`
	Findings    []Finding `json:"findings"`
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
	maxIncl                 string
	hasMaxIncl              bool
	// patLenMin and patLenMax bound the length of a pattern Go's regexp cannot count exactly: XSD allows
	// repeat counts above 1000 (([0-9A-F][0-9A-F]){1,10000}), RE2 does not, so the pattern is compiled
	// with an open bound and the length checked separately.
	patLenMin, patLenMax int
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
	anyNS     string // the wildcard's namespace: ##any, or a namespace such as XML Signature's
	anyStrict bool   // processContents="strict": a matching declaration is demanded
}

type complexType struct {
	name       string
	particles  []particle // sequence or top-level choice entries
	simpleBase string     // for simpleContent: the base simple type
	attrs      []attrDef
	empty      bool // no content model at all: the element must be empty
}

type attrDef struct {
	name     string
	typ      string
	required bool
}

type schema struct {
	ns       string
	root     string            // the type of the Document element
	globals  map[string]string // every global element and its type (Document, or AppHdr in head.001)
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
	Name           string             `xml:"name,attr"`
	Sequence       *xsdGroup          `xml:"sequence"`
	Choice         *xsdGroup          `xml:"choice"`
	SimpleContent  *xsdSimpleContent  `xml:"simpleContent"`
	ComplexContent *xsdComplexContent `xml:"complexContent"`
}

// xsdComplexContent is a complex type derived from another: a restriction restates the content it
// keeps (how usage-guideline schemas narrow a message), an extension adds content after its base's.
type xsdComplexContent struct {
	Restriction *xsdDerivation `xml:"restriction"`
	Extension   *xsdDerivation `xml:"extension"`
}

type xsdDerivation struct {
	Base     string    `xml:"base,attr"`
	Sequence *xsdGroup `xml:"sequence"`
	Choice   *xsdGroup `xml:"choice"`
	Attrs    []xsdAttr `xml:"attribute"`
}

type xsdGroup struct {
	Items []xsdItem `xml:",any"`
}

// xsdItem is one child of a sequence or choice, kept in document order: an element, a nested choice or
// an xs:any wildcard.
type xsdItem struct {
	XMLName         xml.Name
	Name            string    `xml:"name,attr"`
	Type            string    `xml:"type,attr"`
	MinOccurs       string    `xml:"minOccurs,attr"`
	MaxOccurs       string    `xml:"maxOccurs,attr"`
	Namespace       string    `xml:"namespace,attr"`
	ProcessContents string    `xml:"processContents,attr"`
	Items           []xsdItem `xml:",any"`
}

type xsdSimpleContent struct {
	Extension struct {
		Base  string    `xml:"base,attr"`
		Attrs []xsdAttr `xml:"attribute"`
	} `xml:"extension"`
	// A restriction narrows a simple-content type: facets on its value, its attributes restated.
	Restriction *xsdRestriction `xml:"restriction"`
}

type xsdAttr struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Use  string `xml:"use,attr"`
}

type xsdSimple struct {
	Name        string         `xml:"name,attr"`
	Restriction xsdRestriction `xml:"restriction"`
}

type xsdRestriction struct {
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
	MaxInclusive   *facetVal `xml:"maxInclusive"`
	Attrs          []xsdAttr `xml:"attribute"`
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
	return itemParticles(g.Items)
}

// itemParticles turns sequence or choice children into particles, in document order: ISO 20022
// schemas put a choice in the middle of a sequence often enough that order matters.
func itemParticles(items []xsdItem) []particle {
	var ps []particle
	for _, it := range items {
		switch it.XMLName.Local {
		case "element":
			ps = append(ps, particle{name: it.Name, typ: it.Type, min: occurs(it.MinOccurs, 1), max: occurs(it.MaxOccurs, 1)})
		case "any":
			ns := it.Namespace
			if ns == "" {
				ns = "##any"
			}
			ps = append(ps, particle{isAny: true, min: occurs(it.MinOccurs, 1), max: occurs(it.MaxOccurs, 1), anyNS: ns,
				anyStrict: it.ProcessContents == "" || it.ProcessContents == "strict"})
		case "choice":
			ps = append(ps, particle{isChoice: true, min: occurs(it.MinOccurs, 1), max: occurs(it.MaxOccurs, 1), choiceAlt: itemParticles(it.Items)})
		case "sequence":
			ps = append(ps, itemParticles(it.Items)...)
		}
	}
	return ps
}

// rePatternBigRepeat splits a pattern around one atom repeated more than RE2's limit of 1000: a
// fixed-width prefix, the atom (a group, a class or one character), the counts, a fixed-width suffix.
var rePatternBigRepeat = regexp.MustCompile(`^(.*?)(\([^()]*\)|\[[^\]]*\]|\\?.)\{(\d+),(\d+)\}(.*)$`)

// compilePattern compiles an XSD pattern. A pattern that repeats a fixed-width atom more than 1000
// times is compiled with an open upper bound, and the bound returned as a length range instead. Spaces
// in an XSD pattern are literal, as they are here.
func compilePattern(src string) (*regexp.Regexp, int, int, error) {
	re, err := regexp.Compile("^(?:" + src + ")$")
	if err == nil {
		return re, 0, 0, nil
	}
	m := rePatternBigRepeat.FindStringSubmatch(src)
	if m == nil {
		return nil, 0, 0, err
	}
	pre, atom, lo, hi, suf := m[1], m[2], atoi(m[3]), atoi(m[4]), m[5]
	wa, wp, ws := atomWidth(atom), 0, 0
	if pre != "" {
		wp = atomWidth(pre)
	}
	if suf != "" {
		ws = atomWidth(suf)
	}
	if wa <= 0 || wp < 0 || ws < 0 {
		return nil, 0, 0, err
	}
	re, err2 := regexp.Compile("^(?:" + pre + "(?:" + atom + "){" + m[3] + ",}" + suf + ")$")
	if err2 != nil {
		return nil, 0, 0, err
	}
	return re, wp + ws + lo*wa, wp + ws + hi*wa, nil
}

// atomWidth is the fixed number of characters an atom matches, or -1 when it varies.
func atomWidth(atom string) int {
	re, err := syntax.Parse(atom, syntax.Perl)
	if err != nil {
		return -1
	}
	var width func(r *syntax.Regexp) int
	width = func(r *syntax.Regexp) int {
		switch r.Op {
		case syntax.OpLiteral:
			return len(r.Rune)
		case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
			return 1
		case syntax.OpCapture:
			return width(r.Sub[0])
		case syntax.OpConcat:
			n := 0
			for _, s := range r.Sub {
				w := width(s)
				if w < 0 {
					return -1
				}
				n += w
			}
			return n
		}
		return -1
	}
	return width(re)
}

func parseSchema(raw []byte) (*schema, error) {
	var xs xsdSchema
	if err := xml.Unmarshal(raw, &xs); err != nil {
		return nil, err
	}
	q := newQNames(raw, xs.TargetNS)
	s := &schema{ns: xs.TargetNS, globals: map[string]string{}, simples: map[string]*simpleType{}, complexs: map[string]*complexType{}}
	for _, e := range xs.Elements {
		s.globals[e.Name] = q.name(e.Type)
		if e.Name == "Document" {
			s.root = q.name(e.Type)
		}
	}
	content := func(seq, choice *xsdGroup) ([]particle, bool) {
		switch {
		case seq != nil:
			return q.particles(groupParticles(seq, false)), true
		case choice != nil:
			// a top-level choice: exactly one of the alternatives
			return []particle{{isChoice: true, min: 1, max: 1, choiceAlt: q.particles(groupParticles(choice, true))}}, true
		}
		return nil, false
	}
	var extensions []*xsdComplex
	type simpleRestriction struct {
		name, base string
		res        *xsdRestriction
	}
	var simpleRestrictions []simpleRestriction
	for i := range xs.ComplexTypes {
		c := &xs.ComplexTypes[i]
		ct := &complexType{name: c.Name}
		if ps, ok := content(c.Sequence, c.Choice); ok {
			ct.particles = ps
		} else if c.SimpleContent != nil && c.SimpleContent.Restriction != nil {
			simpleRestrictions = append(simpleRestrictions, simpleRestriction{c.Name, q.name(c.SimpleContent.Restriction.Base), c.SimpleContent.Restriction})
		} else if c.SimpleContent != nil {
			ct.simpleBase = q.name(c.SimpleContent.Extension.Base)
			for _, a := range c.SimpleContent.Extension.Attrs {
				ct.attrs = append(ct.attrs, attrDef{name: a.Name, typ: q.name(a.Type), required: a.Use == "required"})
			}
		} else if cc := c.ComplexContent; cc != nil && cc.Restriction != nil {
			if ps, ok := content(cc.Restriction.Sequence, cc.Restriction.Choice); ok {
				ct.particles = ps
			} else {
				ct.empty = true
			}
			for _, a := range cc.Restriction.Attrs {
				ct.attrs = append(ct.attrs, attrDef{name: a.Name, typ: q.name(a.Type), required: a.Use == "required"})
			}
		} else if cc != nil && cc.Extension != nil {
			extensions = append(extensions, c) // completed below, once its base is known
		} else {
			ct.empty = true
		}
		s.complexs[c.Name] = ct
	}
	// An extension's content is its base's, then its own; resolve chains in as many passes as they need.
	for pass := 0; pass < 8 && len(extensions) > 0; pass++ {
		var left []*xsdComplex
		for _, c := range extensions {
			ext := c.ComplexContent.Extension
			base := s.complexs[q.name(ext.Base)]
			if base == nil {
				left = append(left, c)
				continue
			}
			ct := s.complexs[c.Name]
			ct.particles = append(append([]particle(nil), base.particles...), func() []particle { p, _ := content(ext.Sequence, ext.Choice); return p }()...)
			ct.attrs = append(append([]attrDef(nil), base.attrs...), ct.attrs...)
			for _, a := range ext.Attrs {
				ct.attrs = append(ct.attrs, attrDef{name: a.Name, typ: q.name(a.Type), required: a.Use == "required"})
			}
			ct.empty = len(ct.particles) == 0 && base.empty
		}
		extensions = left
	}
	// Some usage-guideline schemas (GEFEG exports) declare the Document type but no Document element.
	if s.root == "" && len(s.globals) == 0 {
		if _, ok := s.complexs["Document"]; ok {
			s.root, s.globals["Document"] = "Document", "Document"
		} else {
			// a header guideline: its root is the AppHdr element
			for name := range s.complexs {
				if reHeaderType.MatchString(name) {
					s.globals["AppHdr"] = name
				}
			}
		}
	}
	for _, st := range xs.SimpleTypes {
		t, err := restrictionType(st.Name, q.name(st.Restriction.Base), st.Restriction)
		if err != nil {
			return nil, err
		}
		s.simples[st.Name] = t
	}
	// A simple-content restriction's value is its base's value type with the new facets: model it as
	// an anonymous simple type over that, once every complex type is known.
	for _, r := range simpleRestrictions {
		base := s.complexs[r.base]
		valueBase := "xs:string"
		if base != nil && base.simpleBase != "" {
			valueBase = base.simpleBase
		} else if _, ok := s.simples[r.base]; ok || strings.HasPrefix(r.base, "xs:") {
			valueBase = r.base
		}
		t, err := restrictionType(r.name+"#value", valueBase, *r.res)
		if err != nil {
			return nil, err
		}
		s.simples[t.name] = t
		ct := s.complexs[r.name]
		ct.simpleBase, ct.empty = t.name, false
		restated := map[string]bool{}
		for _, a := range r.res.Attrs {
			restated[a.Name] = true
			ct.attrs = append(ct.attrs, attrDef{name: a.Name, typ: q.name(a.Type), required: a.Use == "required"})
		}
		if base != nil {
			for _, a := range base.attrs {
				if !restated[a.name] {
					ct.attrs = append(ct.attrs, a)
				}
			}
		}
	}
	return s, nil
}

// restrictionType builds a simple type from a restriction's facets.
func restrictionType(name, base string, r xsdRestriction) (*simpleType, error) {
	t := &simpleType{name: name, base: base}
	for _, e := range r.Enumerations {
		t.facet.enum = append(t.facet.enum, e.Value)
	}
	if v := r.MinLength; v != nil {
		t.facet.minLen, t.facet.hasMinLen = atoi(v.Value), true
	}
	if v := r.MaxLength; v != nil {
		t.facet.maxLen, t.facet.hasMaxLen = atoi(v.Value), true
	}
	if v := r.Length; v != nil {
		t.facet.length, t.facet.hasLength = atoi(v.Value), true
	}
	if v := r.Pattern; v != nil {
		re, lo, hi, err := compilePattern(v.Value)
		if err != nil {
			return nil, fmt.Errorf("simple type %s: pattern %q: %w", name, v.Value, err)
		}
		t.facet.patternSrc, t.facet.pattern, t.facet.patLenMin, t.facet.patLenMax = v.Value, re, lo, hi
	}
	if v := r.TotalDigits; v != nil {
		t.facet.totalDigits, t.facet.hasTotal = atoi(v.Value), true
	}
	if v := r.FractionDigits; v != nil {
		t.facet.fracDigits, t.facet.hasFrac = atoi(v.Value), true
	}
	if v := r.MinInclusive; v != nil {
		t.facet.minIncl, t.facet.hasMinIncl = v.Value, true
	}
	if v := r.MaxInclusive; v != nil {
		t.facet.maxIncl, t.facet.hasMaxIncl = v.Value, true
	}
	return t, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

const xsdNS = "http://www.w3.org/2001/XMLSchema"

var reHeaderType = regexp.MustCompile(`^BusinessApplicationHeaderV\d+$`)

// qnames resolves the prefixed names a schema uses for types: a name in the XML Schema namespace
// becomes xs:NAME, whatever its prefix (xs:, xsd:, none); a name in the target namespace becomes its
// local part, whatever its prefix (GEFEG exports write ct:Type); anything else keeps its {namespace}.
type qnames struct {
	prefixes map[string]string
	target   string
}

func newQNames(raw []byte, target string) qnames {
	q := qnames{prefixes: map[string]string{}, target: target}
	d := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := d.RawToken()
		if err != nil {
			return q
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, a := range se.Attr {
				switch {
				case a.Name.Space == "xmlns":
					q.prefixes[a.Name.Local] = a.Value
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					q.prefixes[""] = a.Value
				}
			}
			return q
		}
	}
}

func (q qnames) name(n string) string {
	if n == "" {
		return ""
	}
	prefix, local, found := strings.Cut(n, ":")
	if !found {
		prefix, local = "", n
	}
	ns, known := q.prefixes[prefix]
	switch {
	case !known && prefix == "xs":
		return "xs:" + local // the conventional prefix, even when undeclared
	case !known && prefix == "":
		return local
	case ns == xsdNS:
		return "xs:" + local
	case ns == q.target:
		return local
	case !known:
		return n
	}
	return "{" + ns + "}" + local
}

func (q qnames) particles(ps []particle) []particle {
	for i := range ps {
		ps[i].typ = q.name(ps[i].typ)
		ps[i].choiceAlt = q.particles(ps[i].choiceAlt)
	}
	return ps
}

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
	ns       string
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
			n := &node{ns: t.Name.Space, name: t.Name.Local, attrs: map[string]string{}}
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
	ids      map[string]bool // xs:ID values seen, which must be unique
	idrefs   []struct{ val, path string }
}

func (v *schemaValidator) add(sev Severity, rule, path, msg string) {
	v.findings = append(v.findings, Finding{Rule: rule, Severity: sev, Path: path, Message: msg})
}

// validateSchema checks doc against schema s. The root element must be Document.
func validateSchema(s *schema, doc *node) []Finding {
	v := &schemaValidator{s: s, ids: map[string]bool{}}
	typ := s.globals[doc.name]
	if typ == "" {
		want := "Document"
		if s.root == "" {
			for name := range s.globals {
				want = name
			}
		}
		v.add(Error, "xsd.root", "/"+doc.name, "root element is not "+want)
		return v.findings
	}
	ct := s.complexs[typ]
	if ct == nil {
		v.add(Error, "xsd.schema", "/"+doc.name, "schema defines no type for "+doc.name)
		return v.findings
	}
	v.checkComplex(doc, ct, "/"+doc.name)
	for _, r := range v.idrefs {
		if !v.ids[r.val] {
			v.add(Error, "xsd.idref", r.path, fmt.Sprintf("%q refers to no ID in the message", r.val))
		}
	}
	return v.findings
}

// checkComplex validates n's children and text against complex type ct.
func (v *schemaValidator) checkComplex(n *node, ct *complexType, path string) {
	if ct.empty {
		if len(n.children) > 0 || strings.TrimSpace(n.text) != "" {
			v.add(Error, "xsd.empty", path, "element "+n.name+" must be empty")
		}
		return
	}
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
	if strings.TrimSpace(n.text) != "" {
		v.add(Error, "xsd.text", path, "element "+n.name+" holds elements, not text")
	}
	v.matchParticles(n.children, ct.particles, path)
}

// matchParticles walks the child elements against an ordered particle list (a sequence), descending
// into each child's type. A choice takes one alternative per occurrence; a wildcard takes elements of
// its namespace without checking them, unless it is strict.
func (v *schemaValidator) matchParticles(children []*node, parts []particle, path string) {
	i := v.consume(children, 0, parts, path)
	for ; i < len(children); i++ {
		v.add(Error, "xsd.unexpected", path+"/"+children[i].name, "unexpected element "+children[i].name)
	}
}

// consume matches particles from children[i:] and returns the index of the first child left over.
func (v *schemaValidator) consume(children []*node, i int, parts []particle, path string) int {
	for pi, p := range parts {
		switch {
		case p.isAny:
			count := 0
			for i < len(children) && (p.max < 0 || count < p.max) && !v.claimedLater(children[i], parts[pi+1:]) {
				c := children[i]
				if p.anyNS != "##any" && c.ns != p.anyNS {
					break
				}
				// A strict wildcard demands a declaration for what it takes. The only declarations a message
				// schema carries are its own global elements; XML Signature's are not bundled.
				if p.anyStrict && !(c.ns == v.s.ns && v.s.globals[c.name] != "") {
					v.add(Error, "xsd.wildcard", path+"/"+c.name, "no declaration is available for "+c.name+", which the strict wildcard demands")
				}
				i++
				count++
			}
			if count < p.min {
				v.add(Error, "xsd.minOccurs", path, "missing the required wildcard content")
			}
		case p.isChoice:
			count := 0
			for p.max < 0 || count < p.max {
				j := v.matchChoice(children, i, p, path)
				if j < 0 {
					break
				}
				i = j
				count++
			}
			if count < p.min {
				names := make([]string, 0, len(p.choiceAlt))
				for _, alt := range p.choiceAlt {
					names = append(names, alt.name)
				}
				v.add(Error, "xsd.choice", path, "exactly one of ["+strings.Join(names, ", ")+"] is required")
			}
		default:
			count := 0
			for i < len(children) && children[i].name == p.name && (p.max < 0 || count < p.max) {
				v.descend(children[i], p.typ, path+"/"+p.name)
				i++
				count++
			}
			if count < p.min {
				v.add(Error, "xsd.minOccurs", path+"/"+p.name, fmt.Sprintf("missing required element %s", p.name))
			}
			if p.max > 0 && count == p.max && i < len(children) && children[i].name == p.name {
				n := count
				for i < len(children) && children[i].name == p.name {
					i++
					n++
				}
				v.add(Error, "xsd.maxOccurs", path+"/"+p.name, fmt.Sprintf("%s occurs %d times, at most %d allowed", p.name, n, p.max))
			}
		}
	}
	return i
}

// matchChoice matches one occurrence of a choice at children[i] and returns the next index, or -1
// when no alternative starts there.
func (v *schemaValidator) matchChoice(children []*node, i int, p particle, path string) int {
	if i >= len(children) {
		return -1
	}
	for _, alt := range p.choiceAlt {
		if alt.isChoice || alt.isAny {
			continue
		}
		if children[i].name != alt.name {
			continue
		}
		count := 0
		for i < len(children) && children[i].name == alt.name {
			if alt.max > 0 && count == alt.max {
				n := count
				for i < len(children) && children[i].name == alt.name {
					i++
					n++
				}
				v.add(Error, "xsd.maxOccurs", path+"/"+alt.name, fmt.Sprintf("%s occurs %d times, at most %d allowed", alt.name, n, alt.max))
				return i
			}
			v.descend(children[i], alt.typ, path+"/"+alt.name)
			i++
			count++
		}
		return i
	}
	return -1
}

// claimedLater reports whether a later particle names this child, so a wildcard does not swallow it.
func (v *schemaValidator) claimedLater(c *node, rest []particle) bool {
	for _, p := range rest {
		if p.name == c.name {
			return true
		}
		for _, alt := range p.choiceAlt {
			if alt.name == c.name {
				return true
			}
		}
	}
	return false
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
// base chain and applying every facet on the way. Whitespace follows XSD: a string keeps it, every
// other built-in type collapses it first.
func (v *schemaValidator) checkSimple(val, typ, path string) {
	if strings.HasPrefix(typ, "xs:") {
		v.checkBuiltin(whitespace(val, typ), typ, path)
		return
	}
	st := v.s.simples[typ]
	if st == nil {
		// unknown named type; validate as a string (lax)
		return
	}
	v.applyFacet(whitespace(val, builtinOf(v.s, typ)), st.facet, path, typ)
	if st.base != "" {
		v.checkSimple(val, st.base, path)
	}
}

// builtinOf follows a simple type's base chain to its xs: builtin.
func builtinOf(s *schema, typ string) string {
	for i := 0; i < 32 && !strings.HasPrefix(typ, "xs:"); i++ {
		st := s.simples[typ]
		if st == nil {
			return "xs:string"
		}
		typ = st.base
	}
	return typ
}

// whitespace applies the built-in type's whiteSpace facet: preserve for xs:string, collapse otherwise.
func whitespace(val, builtin string) string {
	switch builtin {
	case "xs:string", "":
		return val
	case "xs:normalizedString":
		return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(val)
	}
	return strings.Join(strings.Fields(val), " ")
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
	n := utf8.RuneCountInString(val)
	if f.hasLength && n != f.length {
		v.add(Error, "xsd.length", path, fmt.Sprintf("%q must be exactly %d characters", val, f.length))
	}
	if f.hasMinLen && n < f.minLen {
		v.add(Error, "xsd.minLength", path, fmt.Sprintf("%q is shorter than the minimum %d characters", val, f.minLen))
	}
	if f.hasMaxLen && n > f.maxLen {
		v.add(Error, "xsd.maxLength", path, fmt.Sprintf("value of %d characters exceeds the maximum %d", n, f.maxLen))
	}
	if f.pattern != nil && (!f.pattern.MatchString(val) || f.patLenMax > 0 && (n < f.patLenMin || n > f.patLenMax)) {
		v.add(Error, "xsd.pattern", path, fmt.Sprintf("%q does not match the required pattern %s", val, f.patternSrc))
	}
	if f.hasTotal || f.hasFrac || f.hasMinIncl || f.hasMaxIncl {
		v.checkDecimalFacets(val, f, path)
	}
}

func (v *schemaValidator) checkDecimalFacets(val string, f facet, path string) {
	r, ok := new(big.Rat).SetString(val)
	if !ok || !reDecimal.MatchString(val) {
		return // the builtin check reports a non-decimal
	}
	if f.hasMinIncl {
		if m, ok := new(big.Rat).SetString(f.minIncl); ok && r.Cmp(m) < 0 {
			v.add(Error, "xsd.minInclusive", path, fmt.Sprintf("%s is less than the minimum %s", val, f.minIncl))
		}
	}
	if f.hasMaxIncl {
		if m, ok := new(big.Rat).SetString(f.maxIncl); ok && r.Cmp(m) > 0 {
			v.add(Error, "xsd.maxInclusive", path, fmt.Sprintf("%s is more than the maximum %s", val, f.maxIncl))
		}
	}
	intPart, frac, _ := strings.Cut(strings.TrimLeft(val, "+-"), ".")
	frac = strings.TrimRight(frac, "0")
	intPart = strings.TrimLeft(intPart, "0")
	if f.hasTotal && len(intPart)+len(frac) > f.totalDigits {
		v.add(Error, "xsd.totalDigits", path, fmt.Sprintf("%s has more than %d significant digits", val, f.totalDigits))
	}
	if f.hasFrac && len(frac) > f.fracDigits {
		v.add(Error, "xsd.fractionDigits", path, fmt.Sprintf("%s has more than %d fraction digits", val, f.fracDigits))
	}
}

var (
	reDate     = regexp.MustCompile(`^-?\d{4}-\d{2}-\d{2}(Z|[+-]\d{2}:\d{2})?$`)
	reDateTime = regexp.MustCompile(`^-?\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`)
	reYearMon  = regexp.MustCompile(`^-?\d{4}-\d{2}(Z|[+-]\d{2}:\d{2})?$`)
	reTime     = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`)
)

func (v *schemaValidator) checkBuiltin(val, typ, path string) {
	bad := func(rule, what string) { v.add(Error, rule, path, fmt.Sprintf("%q is not %s", val, what)) }
	switch typ {
	case "xs:decimal":
		if !reDecimal.MatchString(val) {
			bad("xsd.decimal", "a decimal number")
		}
	case "xs:positiveInteger":
		if !rePositiveInt.MatchString(val) || strings.Trim(strings.TrimPrefix(val, "+"), "0") == "" {
			bad("xsd.positiveInteger", "a positive integer")
		}
	case "xs:date":
		if !reDate.MatchString(val) || !validDateParts(val) {
			bad("xsd.date", "an ISO date (YYYY-MM-DD)")
		}
	case "xs:dateTime":
		if !reDateTime.MatchString(val) || !validDateParts(val) {
			bad("xsd.dateTime", "an ISO dateTime")
		}
	case "xs:gYearMonth":
		if !reYearMon.MatchString(val) || !validDateParts(val) {
			bad("xsd.gYearMonth", "a year-month")
		}
	case "xs:gYear":
		if !reYear.MatchString(val) {
			bad("xsd.gYear", "a year")
		}
	case "xs:gMonth":
		if m := reMonth.FindStringSubmatch(val); m == nil || atoi(m[1]) < 1 || atoi(m[1]) > 12 {
			bad("xsd.gMonth", "a month (--MM)")
		}
	case "xs:time":
		if !reTime.MatchString(val) || !validTimeParts(val) {
			bad("xsd.time", "a time")
		}
	case "xs:boolean":
		if val != "true" && val != "false" && val != "1" && val != "0" {
			bad("xsd.boolean", "a boolean")
		}
	case "xs:base64Binary":
		if !validBase64(val) {
			bad("xsd.base64Binary", "base64")
		}
	case "xs:ID":
		if !reNCName.MatchString(val) {
			bad("xsd.ID", "an XML name")
		} else if v.ids[val] {
			v.add(Error, "xsd.ID", path, fmt.Sprintf("the ID %q is used twice", val))
		} else {
			v.ids[val] = true
		}
	case "xs:IDREF":
		if !reNCName.MatchString(val) {
			bad("xsd.IDREF", "an XML name")
		} else {
			v.idrefs = append(v.idrefs, struct{ val, path string }{val, path})
		}
	case "xs:string", "xs:normalizedString":
		// no lexical constraint beyond facets
	}
}

var (
	reDecimal     = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`)
	rePositiveInt = regexp.MustCompile(`^\+?\d+$`)
	reYear        = regexp.MustCompile(`^-?\d{4,}(Z|[+-]\d{2}:\d{2})?$`)
	reMonth       = regexp.MustCompile(`^--(\d{2})(Z|[+-]\d{2}:\d{2})?$`)
	reNCName      = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_.\-]*$`)
)

// validBase64 is the XSD base64Binary lexical space: base64 with optional spaces between characters.
func validBase64(val string) bool {
	s := strings.ReplaceAll(val, " ", "")
	if len(s)%4 != 0 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
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
