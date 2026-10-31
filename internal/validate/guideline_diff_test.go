package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGuidelinesAgreeWithLibxml2 checks prova's validator against libxml2 on usage-guideline schemas
// as markets publish them: the EPC's (restriction by complexContent) and TCH's RTP schemas (GEFEG
// exports with prefixed type names and no Document element). For each, samples built from the
// guideline schema and from the base message's schema, and broken variants, are judged against the
// guideline by both. testdata/guidelines/diff.json holds libxml2's verdicts (gen_guidelines.py).
func TestGuidelinesAgreeWithLibxml2(t *testing.T) {
	files, _ := filepath.Glob("testdata/guidelines/*.xsd")
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no guideline fixtures")
	}
	dump := os.Getenv("PROVA_DUMP_GUIDELINES")
	want := map[string]string{}
	if dump == "" {
		raw, err := os.ReadFile("testdata/guidelines/diff.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
	}
	cases, disagree := 0, 0
	got := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		g, err := parseSchema(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if g.root == "" {
			t.Fatalf("%s: no Document", f)
		}
		base, err := schemaForKind(nsKind(g.ns))
		if err != nil {
			t.Fatalf("%s: base schema: %v", f, err)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".xsd")
		var b strings.Builder
		for _, c := range guidelineCases {
			s, variant := g, c
			if strings.HasPrefix(c, "base-") {
				s, variant = base, strings.TrimPrefix(c, "base-")
			}
			inst, ok := catalogueInstance(s, variant)
			if !ok {
				b.WriteByte('-')
				continue
			}
			if dump != "" {
				_ = os.WriteFile(filepath.Join(dump, name+"__"+c+".xml"), inst, 0o644)
			}
			doc, err := parseInstance(inst)
			if err != nil {
				b.WriteByte('x')
				continue
			}
			if len(validateSchema(g, doc)) == 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		got[name] = b.String()
		w := want[name]
		if dump != "" {
			continue
		}
		if len(w) != len(got[name]) {
			t.Errorf("%s: diff.json has %q, prova built %q", name, w, got[name])
			continue
		}
		for i := range w {
			if w[i] == '-' {
				continue
			}
			cases++
			if w[i] != got[name][i] {
				disagree++
				t.Errorf("%s case %s: libxml2 %c, prova %c", name, guidelineCases[i], w[i], got[name][i])
			}
		}
	}
	if dump != "" {
		t.Skip("instances written to " + dump)
	}
	t.Logf("%d cases over %d guideline schemas, %d disagreements", cases, len(files), disagree)
}

// guidelineCases: samples and broken variants of the guideline schema, then the base message's own
// samples, which a guideline usually refuses.
var guidelineCases = append(append([]string(nil), VariantNames...), "base-minimal", "base-full")
