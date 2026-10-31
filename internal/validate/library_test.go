package validate

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestBundledGuidelines(t *testing.T) {
	l := OpenLibrary("")
	gs := l.List()
	if len(gs) != 22 {
		t.Fatalf("%d bundled guidelines, want the 22 TCH RTP schemas", len(gs))
	}
	for _, g := range gs {
		raw, err := l.Schema(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, err := parseSchema(raw)
		if err != nil || s.root == "" && s.globals["AppHdr"] == "" {
			t.Errorf("%s: %v (root %q)", g.ID, err, s.root)
		}
		if _, ok := LookupMessage(g.Kind); !ok && !strings.HasPrefix(g.Kind, "admn.") {
			t.Errorf("%s restricts %q, not a catalogue message", g.ID, g.Kind)
		}
	}
	// A message built from the RTP guideline passes it; the base schema's full sample does not.
	g, err := l.Schema("tch-rtp/pacs.008.001.08")
	if err != nil {
		t.Fatal(err)
	}
	gs8, _ := parseSchema(g)
	ok, _ := catalogueInstance(gs8, "minimal")
	if rep, err := l.MessageWithGuideline(ok, "", "tch-rtp/pacs.008.001.08"); err != nil || !rep.Valid() || rep.GuidelineID == "" {
		t.Errorf("RTP sample: %v %+v", err, rep.Findings)
	}
	full, _ := Variant("pacs.008.001.08", "full")
	if rep, _ := l.MessageWithGuideline(full, "", "tch-rtp/pacs.008.001.08"); rep.Valid() {
		t.Error("the base schema's full sample passed the RTP guideline")
	}
}

func TestImportGuidelines(t *testing.T) {
	dir := t.TempDir()
	l := OpenLibrary(dir)
	epc, err := os.ReadFile("testdata/guidelines/epc-sct-interpsp-2023-pacs.008.001.08.xsd")
	if err != nil {
		t.Fatal(err)
	}
	// One schema on its own.
	rep, err := l.Import("epc-sct", "EPC SCT inter-PSP 2023", "European Payments Council", "EPC115-06_2023_V1.0_pacs.008.001.08_Update.xsd", epc)
	if err != nil || len(rep.Added) != 1 || rep.Added[0].Kind != "pacs.008.001.08" {
		t.Fatalf("import xsd: %v %+v", err, rep)
	}
	// A zip as MyStandards or the EPC ship them: schemas beside documents, one unreadable, one a shared library.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		w, _ := zw.Create(name)
		_, _ = w.Write(data)
	}
	pain, _ := os.ReadFile("testdata/guidelines/epc-sct-c2psp-2025-pain.001.001.09.xsd")
	add("EPC/EPC132-08_2025_V1.0_pain.001.001.09.xsd", pain)
	add("EPC/../../escape/pacs.008.xsd", epc)
	add("EPC/Implementation Guidelines.pdf", []byte("%PDF-1.4"))
	add("EPC/broken.xsd", []byte("<xs:schema"))
	add("EPC/common.xsd", []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:x"><xs:simpleType name="T"><xs:restriction base="xs:string"/></xs:simpleType></xs:schema>`))
	_ = zw.Close()
	rep, err = l.Import("epc-2025", "", "", "SCT-2025.zip", buf.Bytes())
	if err != nil || len(rep.Added) != 2 || len(rep.Skipped) != 3 {
		t.Fatalf("import zip: %v added %+v skipped %+v", err, rep.Added, rep.Skipped)
	}
	if _, err := os.Stat(dir + "/escape"); err == nil {
		t.Fatal("a zip entry escaped the library")
	}
	// TCH's RTP pacs.008.001.08, the EPC set's, and the second set's copy of it
	if got := len(l.ForMessage("pacs.008.001.08")); got != 3 {
		t.Errorf("%d guidelines for pacs.008.001.08, want 3", got)
	}
	for _, bad := range []string{"../x", "tch-rtp", "", "a/b"} {
		if _, err := l.Import(bad, "", "", "x.xsd", epc); err == nil {
			t.Errorf("set %q accepted", bad)
		}
	}
	if _, err := l.Schema("../../etc/passwd"); err == nil {
		t.Error("a path outside the library was read")
	}
	if err := l.Remove("epc-sct"); err != nil {
		t.Fatal(err)
	}
	if err := l.Remove("tch-rtp"); err == nil {
		t.Error("a bundled set was removed")
	}
	if _, err := l.Schema("epc-sct/EPC115-06_2023_V1.0_pacs.008.001.08_Update"); err == nil {
		t.Error("a removed guideline is still there")
	}
}
