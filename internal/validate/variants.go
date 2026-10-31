package validate

import (
	"fmt"
	"strings"
)

// VariantNames names the instances prova builds of every catalogue message: a minimal and a full valid
// sample, then one deliberately broken variant per kind of schema error. diff.json records libxml2's
// verdict on each in this order.
var VariantNames = []string{"minimal", "full", "drop", "extra", "enum", "pattern", "maxlen", "date", "decimal",
	"totaldigits", "dup", "space", "attr", "bool", "order", "minlen"}

// Variant builds one named instance of a message version (see VariantNames). It fails when the schema
// offers nothing that variant can break.
func Variant(kind, name string) ([]byte, error) {
	s, err := schemaForKind(kind)
	if err != nil {
		return nil, err
	}
	raw, ok := catalogueInstance(s, name)
	if !ok {
		return nil, fmt.Errorf("validate: %s has nothing a %q variant can break", kind, name)
	}
	return raw, nil
}

// VariantDescriptions says what each broken variant does to the message.
var VariantDescriptions = map[string]string{
	"minimal": "only what the schema requires", "full": "every optional element once",
	"drop": "a required element removed", "extra": "an element the schema does not define added",
	"enum": "a code that is not in its code list", "pattern": "a value that breaks its pattern",
	"maxlen": "a value one character too long", "date": "an impossible date", "decimal": "a malformed number",
	"totaldigits": "a number with too many digits", "dup": "an element that may occur once given twice",
	"space": "spaces around a text value (still valid: XSD keeps them)", "attr": "a required attribute removed",
	"bool": "a boolean that is neither true nor false", "order": "two required elements swapped",
	"minlen": "an empty value where text is required",
}

// catalogueInstance builds one case for a schema; ok is false when the schema offers nothing to break.
func catalogueInstance(s *schema, name string) ([]byte, bool) {
	mode := SampleFull
	if name == "minimal" {
		mode = SampleMinimal
	}
	root, err := buildSample(s, mode)
	if err != nil {
		return nil, false
	}
	if name == "minimal" || name == "full" {
		return root.render(s.ns), true
	}
	m := &mutator{s: s}
	if !m.apply(root, name) {
		return nil, false
	}
	return root.render(s.ns), true
}

type mutator struct{ s *schema }

// walk visits nodes in document order with their parent; visit returns true to stop.
func walk(n, parent *sampleNode, visit func(n, parent *sampleNode) bool) bool {
	if visit(n, parent) {
		return true
	}
	for _, c := range n.children {
		if walk(c, n, visit) {
			return true
		}
	}
	return false
}

func (m *mutator) simpleFacets(n *sampleNode) (facet, string) {
	g := &sampler{s: m.s}
	return g.facets(n.simple)
}

// particleFor finds the sequence particle (not a choice alternative) a child was generated from.
func (m *mutator) particleFor(parent *sampleNode, child string) (particle, bool) {
	ct := m.s.complexs[parent.typ]
	if ct == nil {
		return particle{}, false
	}
	for _, p := range ct.particles {
		if !p.isChoice && !p.isAny && p.name == child {
			return p, true
		}
	}
	return particle{}, false
}

func (m *mutator) apply(root *sampleNode, name string) bool {
	text := func(pred func(f facet, b string, n *sampleNode) bool, set func(f facet, n *sampleNode)) bool {
		return walk(root, nil, func(n, _ *sampleNode) bool {
			if n.simple == "" || n.typ == "any" || len(n.attrs) > 0 && n.simple == "" {
				return false
			}
			f, b := m.simpleFacets(n)
			if strings.HasPrefix(n.simple, "xs:") {
				b = n.simple
			}
			if pred(f, b, n) {
				set(f, n)
				return true
			}
			return false
		})
	}
	switch name {
	case "drop":
		return walk(root, nil, func(n, _ *sampleNode) bool {
			for i, c := range n.children {
				if p, ok := m.particleFor(n, c.name); ok && p.min == 1 && depthOK(root, n) {
					n.children = append(n.children[:i:i], n.children[i+1:]...)
					return true
				}
			}
			return false
		})
	case "extra":
		var last *sampleNode
		walk(root, nil, func(n, _ *sampleNode) bool {
			if len(n.children) > 0 && n.typ != "any" && !hasWildcard(m.s, n.typ) {
				last = n
			}
			return false
		})
		if last == nil {
			return false
		}
		last.children = append(last.children, &sampleNode{name: "Unexpected", text: "1", simple: "xs:string"})
		return true
	case "enum":
		return text(func(f facet, _ string, _ *sampleNode) bool { return len(f.enum) > 0 },
			func(_ facet, n *sampleNode) { n.text = "ZZZZZZ9" })
	case "pattern":
		return text(func(f facet, _ string, _ *sampleNode) bool { return f.pattern != nil && len(f.enum) == 0 },
			func(_ facet, n *sampleNode) { n.text = "#" })
	case "maxlen":
		return text(func(f facet, _ string, _ *sampleNode) bool { return f.hasMaxLen && f.pattern == nil && f.maxLen < 5000 },
			func(f facet, n *sampleNode) { n.text = strings.Repeat("A", f.maxLen+1) })
	case "minlen":
		return text(func(f facet, _ string, _ *sampleNode) bool {
			return f.hasMinLen && f.minLen > 0 && f.pattern == nil && len(f.enum) == 0
		},
			func(f facet, n *sampleNode) { n.text = "" })
	case "date":
		return text(func(_ facet, b string, _ *sampleNode) bool { return b == "xs:date" || b == "xs:dateTime" },
			func(_ facet, n *sampleNode) { n.text = "2026-13-45" })
	case "decimal":
		return text(func(_ facet, b string, _ *sampleNode) bool { return b == "xs:decimal" },
			func(_ facet, n *sampleNode) { n.text = "1.2.3" })
	case "totaldigits":
		return text(func(f facet, _ string, _ *sampleNode) bool { return f.hasTotal && f.totalDigits < 40 },
			func(f facet, n *sampleNode) { n.text = strings.Repeat("9", f.totalDigits+1) })
	case "space":
		return text(func(f facet, b string, _ *sampleNode) bool {
			return b == "xs:string" && f.pattern == nil && len(f.enum) == 0 && f.hasMaxLen && f.maxLen >= 12
		}, func(_ facet, n *sampleNode) { n.text = " " + n.text + " " })
	case "bool":
		return text(func(_ facet, b string, _ *sampleNode) bool { return b == "xs:boolean" },
			func(_ facet, n *sampleNode) { n.text = "yes" })
	case "dup":
		return walk(root, nil, func(n, _ *sampleNode) bool {
			for i, c := range n.children {
				if p, ok := m.particleFor(n, c.name); ok && p.max == 1 && depthOK(root, n) {
					cp := *c
					n.children = append(n.children[:i+1:i+1], append([]*sampleNode{&cp}, n.children[i+1:]...)...)
					return true
				}
			}
			return false
		})
	case "attr":
		return walk(root, nil, func(n, _ *sampleNode) bool {
			ct := m.s.complexs[n.typ]
			if ct == nil {
				return false
			}
			for _, a := range ct.attrs {
				if a.required {
					for i, have := range n.attrs {
						if have[0] == a.name {
							n.attrs = append(n.attrs[:i:i], n.attrs[i+1:]...)
							return true
						}
					}
				}
			}
			return false
		})
	case "order":
		return walk(root, nil, func(n, _ *sampleNode) bool {
			for i := 0; i+1 < len(n.children); i++ {
				a, b := n.children[i], n.children[i+1]
				pa, oka := m.particleFor(n, a.name)
				pb, okb := m.particleFor(n, b.name)
				if oka && okb && a.name != b.name && pa.min == 1 && pb.min == 1 {
					n.children[i], n.children[i+1] = b, a
					return true
				}
			}
			return false
		})
	}
	return false
}

// depthOK skips the Document element itself, so a mutation breaks the business content.
func depthOK(root, n *sampleNode) bool { return n != root }

func hasWildcard(s *schema, typ string) bool {
	ct := s.complexs[typ]
	if ct == nil {
		return false
	}
	for _, p := range ct.particles {
		if p.isAny {
			return true
		}
	}
	return false
}
