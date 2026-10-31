package validate

import (
	"encoding/xml"
	"fmt"
	"math/big"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"unicode/utf8"
)

// Sample generation: a schema-valid instance of any message in the catalogue, built from its schema.
// The minimal sample carries only what the schema requires, taking the first alternative of every
// choice; the full sample also carries every optional element once, up to a depth, so a reader sees
// the whole shape of the message. Values satisfy every facet: an enumeration's first code, a string
// generated from the pattern, a decimal inside its digit and range bounds. Both kinds are checked
// against libxml2 in the package tests (TestCatalogueAgreesWithLibxml2).

// SampleMode chooses how much of the message a sample carries.
type SampleMode int

const (
	SampleMinimal SampleMode = iota // only what the schema requires
	SampleFull                      // every optional element once, to a depth
)

// maxFullDepth bounds how deep a full sample descends into optional content.
const maxFullDepth = 9

// Sample returns a schema-valid instance of a message version, such as sese.023.001.09.
func Sample(kind string, mode SampleMode) ([]byte, error) {
	s, err := schemaForKind(kind)
	if err != nil {
		return nil, err
	}
	t, err := buildSample(s, mode)
	if err != nil {
		return nil, fmt.Errorf("validate: %s: %w", kind, err)
	}
	return t.render(s.ns), nil
}

// sampleNode is a generated element: a name, then either text or children, plus attributes.
type sampleNode struct {
	name     string
	text     string
	attrs    [][2]string
	children []*sampleNode
	ns       string // a namespace of its own (wildcard content), else the message's
	typ      string // the schema type it was generated from
	simple   string // the simple type of its text, when it has text
}

func (n *sampleNode) render(ns string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	n.write(&b, ns, 0)
	return []byte(b.String())
}

func (n *sampleNode) write(b *strings.Builder, ns string, depth int) {
	b.WriteString(strings.Repeat("  ", depth) + "<" + n.name)
	if depth == 0 {
		b.WriteString(` xmlns="` + ns + `"`)
	} else if n.ns != "" {
		b.WriteString(` xmlns="` + n.ns + `"`)
	}
	for _, a := range n.attrs {
		b.WriteString(" " + a[0] + `="` + escapeXML(a[1]) + `"`)
	}
	switch {
	case len(n.children) > 0:
		b.WriteString(">\n")
		for _, c := range n.children {
			c.write(b, ns, depth+1)
		}
		b.WriteString(strings.Repeat("  ", depth) + "</" + n.name + ">\n")
	case n.text == "" && n.simple == "":
		b.WriteString("/>\n")
	default:
		b.WriteString(">" + escapeXML(n.text) + "</" + n.name + ">\n")
	}
}

func escapeXML(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type sampler struct {
	s     *schema
	mode  SampleMode
	stack map[string]int // complex types being expanded, to stop recursion
	ids   int
	idref []*sampleNode
	idsOf []string

	strictMemo map[string]bool
	memo       map[string]string // generated values by type and element name
}

func buildSample(s *schema, mode SampleMode) (*sampleNode, error) {
	g := &sampler{s: s, mode: mode, stack: map[string]int{}}
	rootName, rootType := "Document", s.root
	if rootType == "" {
		names := make([]string, 0, len(s.globals))
		for n := range s.globals {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			return nil, fmt.Errorf("the schema declares no element")
		}
		rootName, rootType = names[0], s.globals[names[0]]
	}
	root := g.element(rootName, rootType, 0)
	if root == nil {
		return nil, fmt.Errorf("cannot build %s", rootName)
	}
	// An IDREF must point at an ID in the same message; point each at the first ID generated.
	if len(g.idref) > 0 && len(g.idsOf) == 0 {
		return nil, fmt.Errorf("the message refers to IDs but carries none")
	}
	for _, r := range g.idref {
		r.text = g.idsOf[0]
	}
	return root, nil
}

// element builds one element of a named type.
func (g *sampler) element(name, typ string, depth int) *sampleNode {
	n := &sampleNode{name: name, typ: typ}
	ct := g.s.complexs[typ]
	if ct == nil {
		n.simple = typ
		n.text = g.value(typ, name)
		g.noteID(n, typ)
		return n
	}
	if ct.empty {
		return n
	}
	if ct.simpleBase != "" {
		n.simple = ct.simpleBase
		n.text = g.value(ct.simpleBase, name)
		g.noteID(n, ct.simpleBase)
		for _, a := range ct.attrs {
			if a.required || g.mode == SampleFull {
				n.attrs = append(n.attrs, [2]string{a.name, g.value(a.typ, a.name)})
			}
		}
		return n
	}
	g.stack[typ]++
	defer func() { g.stack[typ]-- }()
	n.children = g.particles(ct.particles, depth)
	return n
}

func (g *sampler) noteID(n *sampleNode, typ string) {
	switch builtinOf(g.s, typ) {
	case "xs:ID":
		g.ids++
		n.text = fmt.Sprintf("ID%d", g.ids)
		g.idsOf = append(g.idsOf, n.text)
	case "xs:IDREF":
		g.idref = append(g.idref, n)
	}
}

func (g *sampler) particles(parts []particle, depth int) []*sampleNode {
	var out []*sampleNode
	for _, p := range parts {
		switch {
		case p.isAny:
			// Wildcard content is free; a required one gets an element in the message's namespace.
			if p.min > 0 {
				w := &sampleNode{name: "Data", simple: "xs:string", typ: "any"}
				if p.anyNS != "##any" {
					w.name, w.ns = "Signature", p.anyNS
				}
				out = append(out, w)
			}
		case p.isChoice:
			if p.min == 0 && !g.optional(depth) {
				continue
			}
			alt := g.pickAlternative(p.choiceAlt, depth)
			if alt.name == "" {
				continue
			}
			n := 1
			if alt.min > 1 {
				n = alt.min
			}
			for i := 0; i < n; i++ {
				out = append(out, g.element(alt.name, alt.typ, depth+1))
			}
		default:
			n := p.min
			if n == 0 && g.optional(depth) && g.stack[p.typ] == 0 && !g.needsForeign(p.typ) {
				n = 1
			}
			for i := 0; i < n; i++ {
				out = append(out, g.element(p.name, p.typ, depth+1))
			}
		}
	}
	return out
}

// needsForeign reports whether a type demands, somewhere in its required content, an element through
// a strict wildcard: XML Signature's or another message's, neither of which a message schema declares.
// No instance can carry it validly, so a full sample leaves such optional elements out.
func (g *sampler) needsForeign(typ string) bool {
	if g.strictMemo == nil {
		g.strictMemo = map[string]bool{}
	}
	if v, ok := g.strictMemo[typ]; ok {
		return v
	}
	g.strictMemo[typ] = false // a type being checked does not count against itself
	ct := g.s.complexs[typ]
	found := false
	if ct != nil {
		for _, p := range ct.particles {
			switch {
			case p.isAny:
				found = p.anyStrict && p.min > 0
			case p.isChoice:
				if p.min > 0 {
					all := len(p.choiceAlt) > 0
					for _, a := range p.choiceAlt {
						all = all && g.needsForeign(a.typ)
					}
					found = all
				}
			default:
				found = p.min > 0 && g.needsForeign(p.typ)
			}
			if found {
				break
			}
		}
	}
	g.strictMemo[typ] = found
	return found
}

func (g *sampler) optional(depth int) bool { return g.mode == SampleFull && depth < maxFullDepth }

// pickAlternative takes the first alternative whose type is not already being expanded (a choice is
// how ISO 20022 schemas recurse, so the first is not always safe).
func (g *sampler) pickAlternative(alts []particle, depth int) particle {
	for _, a := range alts {
		if a.isAny || a.isChoice {
			continue
		}
		if g.stack[a.typ] == 0 {
			return a
		}
	}
	for _, a := range alts {
		if !a.isAny && !a.isChoice {
			return a
		}
	}
	return particle{}
}

// --- values --------------------------------------------------------------------------------

// friendly values for common ISO 20022 types, used when they satisfy the type's facets.
var friendly = map[string]string{
	"ActiveCurrencyCode": "EUR", "ActiveOrHistoricCurrencyCode": "EUR", "CurrencyCode": "EUR",
	"BICFIDec2014Identifier": "BANKDEFFXXX", "AnyBICDec2014Identifier": "BANKDEFFXXX", "BICFIIdentifier": "BANKDEFFXXX",
	"AnyBICIdentifier": "BANKDEFFXXX", "BICIdentifier": "BANKDEFFXXX", "BICFI2013Identifier": "BANKDEFFXXX",
	"IBAN2007Identifier": "DE89370400440532013000", "IBANIdentifier": "DE89370400440532013000",
	"CountryCode": "DE", "LEIIdentifier": "5493001KJTIIGC8Y1R12", "UUIDv4Identifier": "eb6305c9-1f7f-49de-aed0-16487c27b42d",
	"ISINOct2015Identifier": "US0378331005", "ISINIdentifier": "US0378331005", "ISIN2021Identifier": "US0378331005",
	"Exact4AlphaNumericText": "TEST", "PhoneNumber": "+49-123456789",
}

var friendlyBuiltin = map[string]string{
	"xs:date": "2026-10-15", "xs:dateTime": "2026-10-15T10:00:00", "xs:time": "10:00:00", "xs:gYearMonth": "2026-10",
	"xs:gYear": "2026", "xs:gMonth": "--10", "xs:boolean": "true", "xs:base64Binary": "cHJvdmE=", "xs:positiveInteger": "1",
	"xs:ID": "ID1", "xs:IDREF": "ID1",
}

// builtinCandidates are values tried in turn for a type derived from a date or time builtin, whose
// pattern may demand a time zone.
var builtinCandidates = map[string][]string{
	"xs:dateTime": {"2026-10-15T10:00:00", "2026-10-15T10:00:00Z", "2026-10-15T10:00:00.000Z", "2026-10-15T10:00:00+00:00"},
	"xs:date":     {"2026-10-15", "2026-10-15Z"},
	"xs:time":     {"10:00:00", "10:00:00Z", "10:00:00.000Z"},
}

// value returns a valid value for a simple type: its friendly value, its first enumeration, or one
// built to satisfy its pattern, length and numeric facets.
func (g *sampler) value(typ, element string) string {
	key := typ + "|" + element
	if v, ok := g.memo[key]; ok {
		return v
	}
	v := g.computeValue(typ, element)
	if g.memo == nil {
		g.memo = map[string]string{}
	}
	g.memo[key] = v
	return v
}

func (g *sampler) computeValue(typ, element string) string {
	if strings.HasPrefix(typ, "xs:") {
		if v, ok := friendlyBuiltin[typ]; ok {
			return v
		}
		if typ == "xs:decimal" {
			return "1"
		}
		return "Text"
	}
	f, builtin := g.facets(typ)
	if v, ok := friendly[typ]; ok && g.fits(v, typ) {
		return v
	}
	if len(f.enum) > 0 {
		return f.enum[0]
	}
	if builtin == "xs:decimal" || f.hasTotal || f.hasFrac || f.hasMinIncl || f.hasMaxIncl {
		return decimalValue(f)
	}
	for _, v := range builtinCandidates[builtin] {
		if g.fits(v, typ) {
			return v
		}
	}
	if v, ok := friendlyBuiltin[builtin]; ok && builtin != "xs:ID" && builtin != "xs:IDREF" {
		return v
	}
	if builtin == "xs:ID" || builtin == "xs:IDREF" {
		return "ID1"
	}
	var cands []string
	if f.pattern != nil {
		re, err := syntax.Parse(rewriteBigRepeat(f.patternSrc), syntax.Perl)
		if err != nil {
			return "A"
		}
		first := ""
		for stretch := 0; stretch < 48; stretch++ {
			v := patternString(re, stretch)
			if stretch == 0 {
				first = v
			}
			if w, ok := fitLength(v, f, true); ok && g.fits(w, typ) {
				return w
			}
		}
		return first
	} else {
		word := "Text"
		if strings.Contains(typ, "Max") || strings.Contains(typ, "Text") {
			word = sampleWord(element)
		}
		cands = append(cands, word, "A")
	}
	for _, c := range cands {
		if v, ok := fitLength(c, f, f.pattern != nil); ok && g.fits(v, typ) {
			return v
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return "A"
}

// sampleWord is a readable text value for an element, from its name.
func sampleWord(element string) string {
	switch element {
	case "MsgId", "Id", "InstrId", "EndToEndId", "TxId", "OrgnlMsgId":
		return "PROVA-1"
	case "Nm":
		return "Example Name"
	case "Ustrd":
		return "Invoice 42"
	}
	return "Text"
}

// facets merges a simple type's facets along its base chain (the nearest wins) and returns its builtin.
func (g *sampler) facets(typ string) (facet, string) {
	var f facet
	seen := map[string]bool{}
	for !strings.HasPrefix(typ, "xs:") && !seen[typ] {
		seen[typ] = true
		st := g.s.simples[typ]
		if st == nil {
			return f, "xs:string"
		}
		h := st.facet
		if len(f.enum) == 0 {
			f.enum = h.enum
		}
		if !f.hasLength && h.hasLength {
			f.length, f.hasLength = h.length, true
		}
		if !f.hasMinLen && h.hasMinLen {
			f.minLen, f.hasMinLen = h.minLen, true
		}
		if !f.hasMaxLen && h.hasMaxLen {
			f.maxLen, f.hasMaxLen = h.maxLen, true
		}
		if f.pattern == nil && h.pattern != nil {
			f.pattern, f.patternSrc, f.patLenMin, f.patLenMax = h.pattern, h.patternSrc, h.patLenMin, h.patLenMax
		}
		if !f.hasTotal && h.hasTotal {
			f.totalDigits, f.hasTotal = h.totalDigits, true
		}
		if !f.hasFrac && h.hasFrac {
			f.fracDigits, f.hasFrac = h.fracDigits, true
		}
		if !f.hasMinIncl && h.hasMinIncl {
			f.minIncl, f.hasMinIncl = h.minIncl, true
		}
		if !f.hasMaxIncl && h.hasMaxIncl {
			f.maxIncl, f.hasMaxIncl = h.maxIncl, true
		}
		typ = st.base
	}
	return f, typ
}

// fits checks a value against the type with the validator itself.
func (g *sampler) fits(v, typ string) bool {
	sv := &schemaValidator{s: g.s, ids: map[string]bool{}}
	sv.checkSimple(v, typ, "")
	return len(sv.findings) == 0
}

// fitLength pads or trims a value into the length facets. A value built from a pattern is only
// accepted as it is, since padding would break the pattern.
func fitLength(v string, f facet, patterned bool) (string, bool) {
	n := utf8.RuneCountInString(v)
	lo, hi := 0, -1
	if f.hasMinLen {
		lo = f.minLen
	}
	if f.hasMaxLen {
		hi = f.maxLen
	}
	if f.hasLength {
		lo, hi = f.length, f.length
	}
	if n >= lo && (hi < 0 || n <= hi) {
		return v, true
	}
	if patterned {
		return "", false
	}
	if n < lo {
		return v + strings.Repeat("A", lo-n), true
	}
	return string([]rune(v)[:hi]), true
}

// decimalValue is a number inside the digit and range facets: 1, or the minimum or maximum when 1
// falls outside them, written with as many fraction digits as the minimum needs.
func decimalValue(f facet) string {
	one := big.NewRat(1, 1)
	v := one
	if f.hasMinIncl {
		if m, ok := new(big.Rat).SetString(f.minIncl); ok && v.Cmp(m) < 0 {
			v = m
		}
	}
	if f.hasMaxIncl {
		if m, ok := new(big.Rat).SetString(f.maxIncl); ok && v.Cmp(m) > 0 {
			v = m
		}
	}
	if v.IsInt() {
		return v.Num().String()
	}
	frac := 6
	if f.hasFrac {
		frac = f.fracDigits
	}
	return strings.TrimRight(strings.TrimRight(v.FloatString(frac), "0"), ".")
}

// patternString generates a string matching an XSD pattern. Every repetition is taken stretch
// times beyond its minimum (within its maximum), so a caller can try longer strings to meet a length
// facet. Classes give their first letter or digit.
func patternString(re *syntax.Regexp, stretch int) string {
	var b strings.Builder
	var gen func(r *syntax.Regexp)
	gen = func(r *syntax.Regexp) {
		switch r.Op {
		case syntax.OpLiteral:
			b.WriteString(string(r.Rune))
		case syntax.OpCharClass:
			b.WriteRune(pickRune(r.Rune))
		case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
			b.WriteByte('A')
		case syntax.OpCapture:
			gen(r.Sub[0])
		case syntax.OpConcat:
			for _, s := range r.Sub {
				gen(s)
			}
		case syntax.OpAlternate:
			gen(r.Sub[0])
		case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
			lo, hi := 0, -1
			switch r.Op {
			case syntax.OpPlus:
				lo = 1
			case syntax.OpQuest:
				hi = 1
			case syntax.OpRepeat:
				lo, hi = r.Min, r.Max
			}
			n := lo + stretch
			if hi >= 0 && n > hi {
				n = hi
			}
			for i := 0; i < n; i++ {
				gen(r.Sub[0])
			}
		}
	}
	gen(re)
	return b.String()
}

// rewriteBigRepeat lowers repeat counts above RE2's 1000 so a long-binary pattern can be generated.
var reBigCount = regexp.MustCompile(`\{(\d+),(\d{4,})\}`)

func rewriteBigRepeat(src string) string {
	return reBigCount.ReplaceAllString(src, "{$1,1000}")
}

// pickRune chooses a readable rune from a class's ranges: a capital letter, then a digit, then a small
// letter, then the first in the class.
func pickRune(ranges []rune) rune {
	for _, want := range []rune{'A', '1', 'a', '0'} {
		for i := 0; i+1 < len(ranges); i += 2 {
			if ranges[i] <= want && want <= ranges[i+1] {
				return want
			}
		}
	}
	for i := 0; i+1 < len(ranges); i += 2 {
		for r := ranges[i]; r <= ranges[i+1]; r++ {
			if r > ' ' && r < 0x7f {
				return r
			}
		}
	}
	if len(ranges) > 0 {
		return ranges[0]
	}
	return 'A'
}
