package validate

import (
	"encoding/json"
	"os"
	"testing"
)

// TestCatalogueIndex parses every schema in the catalogue and checks the committed index lists each
// one as it is. PROVA_REGEN_CATALOGUE=1 rewrites the index.
func TestCatalogueIndex(t *testing.T) {
	got, err := buildIndex()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PROVA_REGEN_CATALOGUE") != "" {
		// one message per line, so a change to the catalogue reads as a short diff
		var b []byte
		b = append(b, "[\n"...)
		for i, m := range got {
			line, _ := json.Marshal(m)
			b = append(b, line...)
			if i < len(got)-1 {
				b = append(b, ',')
			}
			b = append(b, '\n')
		}
		raw := append(b, "]\n"...)
		if err := os.WriteFile("catalogue/index.json", raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var want []CatalogueMessage
	if err := json.Unmarshal(catalogueIndex, &want); err != nil && os.Getenv("PROVA_REGEN_CATALOGUE") == "" {
		t.Fatal(err)
	}
	if os.Getenv("PROVA_REGEN_CATALOGUE") == "" {
		if len(got) != len(want) {
			t.Fatalf("index lists %d messages, the catalogue has %d", len(want), len(got))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: index %+v, catalogue %+v", got[i].Kind, want[i], got[i])
			}
		}
	}
	areas, bases := map[string]bool{}, map[string]bool{}
	for _, m := range got {
		areas[m.Area], bases[m.Base] = true, true
		if Areas[m.Area] == "" {
			t.Errorf("%s: business area %s has no name", m.Kind, m.Area)
		}
		if m.Root == "" || m.Name == "" {
			t.Errorf("%s: no root element or name", m.Kind)
		}
	}
	t.Logf("%d versions of %d messages in %d business areas", len(got), len(bases), len(areas))
}
