package validate

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The usage-guideline library: restricted schemas a market infrastructure or community publishes for
// the messages it accepts. prova bundles the ones whose licence allows it (TCH's RTP schemas, through
// moov-io/rtp20022, Apache-2.0) and keeps any others you import (a single XSD, or the ZIP a MyStandards
// or EPC download comes as) in the data directory, under guidelines/. Every schema in the library is
// parsed when it is added, so one that prova cannot read is refused then, not when a message arrives.

//go:embed guidelines
var bundledGuidelines embed.FS

// Guideline is one usage-guideline schema in the library.
type Guideline struct {
	ID        string `json:"id"`        // SET/FILE, e.g. tch-rtp/pacs.008.001.08
	Set       string `json:"set"`       // the collection it came in
	SetName   string `json:"setName"`   // e.g. TCH RTP
	Publisher string `json:"publisher"` // who publishes the guideline
	Kind      string `json:"kind"`      // the message version it restricts
	File      string `json:"file"`      // the schema's file name
	Bundled   bool   `json:"bundled"`   // shipped with prova, not imported
	Source    string `json:"source,omitempty"`
	Imported  string `json:"imported,omitempty"` // when, RFC 3339
}

type guidelineSet struct {
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	Source    string `json:"source,omitempty"`
	Imported  string `json:"imported,omitempty"`
}

var bundledSets = map[string]guidelineSet{
	"tch-rtp": {Name: "TCH RTP", Publisher: "The Clearing House",
		Source: "moov-io/rtp20022 (Apache-2.0), the schemas used for RTP releases 2.9 and 3.0"},
}

// Library is the bundled guidelines and the ones imported into a data directory.
type Library struct {
	dir string // DATA/guidelines; empty for the bundled library only
	mu  sync.Mutex
}

// OpenLibrary returns the library kept under dataDir (created on the first import). An empty dataDir
// gives the bundled guidelines only.
func OpenLibrary(dataDir string) *Library {
	if dataDir == "" {
		return &Library{}
	}
	return &Library{dir: filepath.Join(dataDir, "guidelines")}
}

// List returns every guideline, bundled first, by id.
func (l *Library) List() []Guideline {
	var out []Guideline
	for set, meta := range bundledSets {
		ents, _ := bundledGuidelines.ReadDir("guidelines/" + set)
		for _, e := range ents {
			if g, ok := describe(set, meta, e.Name(), true, func() ([]byte, error) {
				return bundledGuidelines.ReadFile("guidelines/" + set + "/" + e.Name())
			}); ok {
				out = append(out, g)
			}
		}
	}
	if l.dir != "" {
		sets, _ := os.ReadDir(l.dir)
		for _, s := range sets {
			if !s.IsDir() {
				continue
			}
			meta := readSetMeta(filepath.Join(l.dir, s.Name()))
			files, _ := os.ReadDir(filepath.Join(l.dir, s.Name()))
			for _, f := range files {
				p := filepath.Join(l.dir, s.Name(), f.Name())
				if g, ok := describe(s.Name(), meta, f.Name(), false, func() ([]byte, error) { return os.ReadFile(p) }); ok {
					out = append(out, g)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bundled != out[j].Bundled {
			return out[i].Bundled
		}
		return out[i].ID < out[j].ID
	})
	return out
}

var kindCache sync.Map // id -> kind, so List does not reparse every schema

func describe(set string, meta guidelineSet, file string, bundled bool, read func() ([]byte, error)) (Guideline, bool) {
	if !strings.HasSuffix(file, ".xsd") {
		return Guideline{}, false
	}
	id := set + "/" + strings.TrimSuffix(file, ".xsd")
	kind, ok := kindCache.Load(id)
	if !ok {
		raw, err := read()
		if err != nil {
			return Guideline{}, false
		}
		kind = nsKind(schemaNamespace(raw))
		kindCache.Store(id, kind)
	}
	return Guideline{ID: id, Set: set, SetName: meta.Name, Publisher: meta.Publisher, Kind: kind.(string), File: file,
		Bundled: bundled, Source: meta.Source, Imported: meta.Imported}, true
}

var reTargetNS = regexp.MustCompile(`targetNamespace="([^"]+)"`)

func schemaNamespace(raw []byte) string {
	if m := reTargetNS.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

func readSetMeta(dir string) guidelineSet {
	var m guidelineSet
	raw, err := os.ReadFile(filepath.Join(dir, "set.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	if m.Name == "" {
		m.Name = filepath.Base(dir)
	}
	return m
}

// Schema returns a guideline's XSD.
func (l *Library) Schema(id string) ([]byte, error) {
	set, file, ok := strings.Cut(id, "/")
	if !ok || !validName(set) || !validName(file) {
		return nil, fmt.Errorf("validate: no guideline %q", id)
	}
	if _, bundled := bundledSets[set]; bundled {
		raw, err := bundledGuidelines.ReadFile("guidelines/" + set + "/" + file + ".xsd")
		if err != nil {
			return nil, fmt.Errorf("validate: no guideline %q", id)
		}
		return raw, nil
	}
	if l.dir == "" {
		return nil, fmt.Errorf("validate: no guideline %q", id)
	}
	raw, err := os.ReadFile(filepath.Join(l.dir, set, file+".xsd"))
	if err != nil {
		return nil, fmt.Errorf("validate: no guideline %q", id)
	}
	return raw, nil
}

// ForMessage lists the guidelines that restrict a message version.
func (l *Library) ForMessage(kind string) []Guideline {
	var out []Guideline
	for _, g := range l.List() {
		if g.Kind == kind {
			out = append(out, g)
		}
	}
	return out
}

var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+&-]{0,119}$`)

func validName(s string) bool { return reName.MatchString(s) && !strings.Contains(s, "..") }

// ImportReport says what an import added and what it left out, and why.
type ImportReport struct {
	Set     string       `json:"set"`
	Added   []Guideline  `json:"added"`
	Skipped []ImportSkip `json:"skipped"`
}

// ImportSkip is a file an import did not add.
type ImportSkip struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// maxImport caps an imported file or archive, and maxImportFiles how many schemas one may hold.
const (
	maxImport      = 32 << 20
	maxImportFiles = 500
)

// Import adds a guideline set: an .xsd, or a .zip holding XSDs (a MyStandards or EPC download; other
// files in it, such as PDFs and spreadsheets, are skipped). A set of the same name is replaced. Each
// schema must parse, declare a Document and restrict a message in the ISO 20022 catalogue.
func (l *Library) Import(set, name, publisher, fileName string, data []byte) (ImportReport, error) {
	if l.dir == "" {
		return ImportReport{}, fmt.Errorf("validate: the library has no data directory")
	}
	if !validName(set) {
		return ImportReport{}, fmt.Errorf("validate: a set id is letters, digits, dot, dash or underscore, up to 120")
	}
	if _, bundled := bundledSets[set]; bundled {
		return ImportReport{}, fmt.Errorf("validate: %s is a bundled set", set)
	}
	if len(data) > maxImport {
		return ImportReport{}, fmt.Errorf("validate: the file is larger than %d MB", maxImport>>20)
	}
	files := map[string][]byte{}
	rep := ImportReport{Set: set}
	switch strings.ToLower(path.Ext(fileName)) {
	case ".zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return ImportReport{}, fmt.Errorf("validate: not a readable zip: %w", err)
		}
		var total int64
		for _, f := range zr.File {
			base := path.Base(f.Name)
			if f.FileInfo().IsDir() || strings.HasPrefix(base, ".") || strings.HasPrefix(f.Name, "__MACOSX/") {
				continue
			}
			if !strings.EqualFold(path.Ext(base), ".xsd") {
				rep.Skipped = append(rep.Skipped, ImportSkip{File: f.Name, Reason: "not a schema"})
				continue
			}
			if len(files) >= maxImportFiles {
				return ImportReport{}, fmt.Errorf("validate: the archive holds more than %d schemas", maxImportFiles)
			}
			rc, err := f.Open()
			if err != nil {
				rep.Skipped = append(rep.Skipped, ImportSkip{File: f.Name, Reason: err.Error()})
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(rc, maxImport+1))
			rc.Close()
			total += int64(len(raw))
			if err != nil || len(raw) > maxImport || total > 4*maxImport {
				return ImportReport{}, fmt.Errorf("validate: the archive unpacks to more than prova accepts")
			}
			files[base] = raw
		}
	case ".xsd", ".xml":
		files[path.Base(fileName)] = data
	default:
		return ImportReport{}, fmt.Errorf("validate: import an .xsd or a .zip of them")
	}
	keep := map[string][]byte{}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		raw := files[n]
		s, err := parseSchema(raw)
		switch {
		case err != nil:
			rep.Skipped = append(rep.Skipped, ImportSkip{File: n, Reason: "prova cannot read it: " + err.Error()})
			continue
		case s.root == "":
			rep.Skipped = append(rep.Skipped, ImportSkip{File: n, Reason: "it declares no Document (a shared type library, or a wrapper)"})
			continue
		}
		kind := nsKind(s.ns)
		if _, ok := LookupMessage(kind); !ok {
			rep.Skipped = append(rep.Skipped, ImportSkip{File: n, Reason: "its namespace " + orNone(s.ns) + " is not a message in the ISO 20022 catalogue"})
			continue
		}
		file := safeFileName(strings.TrimSuffix(n, path.Ext(n)))
		for keep[file+".xsd"] != nil {
			file += "_"
		}
		keep[file+".xsd"] = raw
	}
	if len(keep) == 0 {
		return rep, fmt.Errorf("validate: nothing to import: no usage-guideline schema prova can use")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	tmp := filepath.Join(l.dir, "."+set+".new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return ImportReport{}, err
	}
	for f, raw := range keep {
		if err := os.WriteFile(filepath.Join(tmp, f), raw, 0o600); err != nil {
			_ = os.RemoveAll(tmp)
			return ImportReport{}, err
		}
	}
	if name == "" {
		name = set
	}
	meta, _ := json.MarshalIndent(guidelineSet{Name: name, Publisher: publisher, Source: path.Base(fileName),
		Imported: time.Now().UTC().Format(time.RFC3339)}, "", " ")
	if err := os.WriteFile(filepath.Join(tmp, "set.json"), meta, 0o600); err != nil {
		_ = os.RemoveAll(tmp)
		return ImportReport{}, err
	}
	final := filepath.Join(l.dir, set)
	_ = os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		return ImportReport{}, err
	}
	forgetSet(set)
	for _, g := range l.List() {
		if g.Set == set {
			rep.Added = append(rep.Added, g)
		}
	}
	return rep, nil
}

// Remove deletes an imported set.
func (l *Library) Remove(set string) error {
	if _, bundled := bundledSets[set]; bundled {
		return fmt.Errorf("validate: %s is bundled and cannot be removed", set)
	}
	if l.dir == "" || !validName(set) {
		return fmt.Errorf("validate: no guideline set %q", set)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p := filepath.Join(l.dir, set)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("validate: no guideline set %q", set)
	}
	forgetSet(set)
	return os.RemoveAll(p)
}

func forgetSet(set string) {
	kindCache.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), set+"/") {
			kindCache.Delete(k)
		}
		return true
	})
}

var reUnsafe = regexp.MustCompile(`[^A-Za-z0-9._+&-]+`)

func safeFileName(s string) string {
	s = strings.Trim(reUnsafe.ReplaceAllString(s, "_"), "._")
	if s == "" {
		s = "schema"
	}
	if len(s) > 110 {
		s = s[:110]
	}
	return s
}

// MessageWithGuideline validates a message against its schema, a profile and a guideline from the
// library.
func (l *Library) MessageWithGuideline(raw []byte, profile, id string) (Report, error) {
	xsd, err := l.Schema(id)
	if err != nil {
		return Report{}, err
	}
	rep, err := MessageWith(raw, profile, xsd)
	if err == nil {
		rep.GuidelineID = id
	}
	return rep, err
}
