package validate

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed samples/*.xml
var sampleFS embed.FS

// Example is a named sample message for the interface to offer.
type Example struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	XML  string `json:"xml"`
}

// Examples returns the bundled valid sample messages plus a couple of deliberately broken ones, so a
// user can see both a clean pass and real findings without supplying their own message.
func Examples() []Example {
	var out []Example
	_ = fs.WalkDir(sampleFS, "samples", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xml") {
			return err
		}
		raw, _ := sampleFS.ReadFile(path)
		name := strings.TrimSuffix(strings.TrimPrefix(path, "samples/"), ".xml")
		out = append(out, Example{Name: name, Kind: nsKind(docNamespace(raw)), XML: string(raw)})
		return nil
	})
	// Two broken variants of the pacs.008 sample: one with a schema error, one CBPR+ only.
	if base := exampleXML(out, "pacs.008-valid"); base != "" {
		out = append(out,
			Example{Name: "pacs.008-bad-schema", Kind: "pacs.008.001.08",
				XML: strings.Replace(base, "<ChrgBr>SHAR</ChrgBr>", "<ChrgBr>WRONG</ChrgBr>", 1)},
			Example{Name: "pacs.008-unstructured-address", Kind: "pacs.008.001.08",
				XML: strings.Replace(base, "<PstlAdr><TwnNm>Paris</TwnNm><Ctry>FR</Ctry></PstlAdr>",
					"<PstlAdr><AdrLine>12 Rue de la Paix</AdrLine></PstlAdr>", 1)},
		)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func exampleXML(xs []Example, name string) string {
	for _, e := range xs {
		if e.Name == name {
			return e.XML
		}
	}
	return ""
}
