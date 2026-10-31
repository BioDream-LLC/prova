package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var catalogueCases = VariantNames

// The catalogue differential test: for every one of the 2,641 schemas, a minimal and a full sample and
// a family of deliberately broken variants, each judged by prova's validator and by libxml2 (through
// lxml). catalogue/diff.json holds libxml2's verdicts; gen_catalogue.py regenerates it from the
// instances PROVA_DUMP_CATALOGUE=DIR writes.

// TestCatalogueAgreesWithLibxml2 compares prova's verdict with libxml2's on every case of every
// schema in the catalogue.
func TestCatalogueAgreesWithLibxml2(t *testing.T) {
	dump := os.Getenv("PROVA_DUMP_CATALOGUE")
	var want map[string]string
	if dump == "" {
		raw, err := os.ReadFile("catalogue/diff.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
	}
	kinds := Catalogue()
	type result struct {
		kind    string
		verdict string
	}
	results := make([]result, len(kinds))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				kind := kinds[i].Kind
				raw, err := CatalogueSchema(kind)
				if err != nil {
					t.Error(err)
					continue
				}
				s, err := parseSchema(raw)
				if err != nil {
					t.Error(kind, err)
					continue
				}
				var b strings.Builder
				for _, c := range catalogueCases {
					inst, ok := catalogueInstance(s, c)
					if !ok {
						b.WriteByte('-')
						continue
					}
					if dump != "" {
						_ = os.WriteFile(filepath.Join(dump, kind+"__"+c+".xml"), inst, 0o644)
					}
					doc, err := parseInstance(inst)
					if err != nil {
						b.WriteByte('x')
						continue
					}
					if fs := validateSchema(s, doc); len(fs) == 0 {
						b.WriteByte('1')
					} else {
						b.WriteByte('0')
					}
				}
				results[i] = result{kind, b.String()}
			}
		}()
	}
	for i := range kinds {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if dump != "" {
		got := map[string]string{}
		for _, r := range results {
			got[r.kind] = r.verdict
		}
		raw, _ := json.MarshalIndent(got, "", " ")
		_ = os.WriteFile(filepath.Join(dump, "prova.json"), raw, 0o644)
		t.Skip("instances written to " + dump)
	}
	cases, disagree := 0, 0
	for _, r := range results {
		w := want[r.kind]
		if len(w) != len(r.verdict) {
			t.Errorf("%s: diff.json has %q, prova built %q", r.kind, w, r.verdict)
			continue
		}
		for i := range w {
			if w[i] == '-' && r.verdict[i] == '-' {
				continue
			}
			cases++
			if w[i] != r.verdict[i] {
				disagree++
				if disagree <= 30 {
					t.Errorf("%s case %s: libxml2 %c, prova %c", r.kind, catalogueCases[i], w[i], r.verdict[i])
				}
			}
		}
	}
	t.Logf("%d cases over %d schemas, %d disagreements", cases, len(results), disagree)
	if disagree > 0 {
		t.Fail()
	}
}
