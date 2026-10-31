package validate

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// The ISO 20022 catalogue: every version of every message definition the ISO 20022 Registration
// Authority publishes (2,641 schemas for 707 messages in 31 business areas), as downloaded from
// iso20022.org's message-set archive. They are kept compressed in one zip and parsed on demand, so a
// message prova does not speak still validates against its official schema, gets a generated sample,
// and can be sent to a target.

//go:embed catalogue/iso20022-schemas.zip
var catalogueZip []byte

//go:embed catalogue/index.json
var catalogueIndex []byte

// CatalogueMessage is one message version in the catalogue.
type CatalogueMessage struct {
	Kind      string `json:"kind"`      // pacs.008.001.08
	Namespace string `json:"namespace"` // the schema's target namespace
	Base      string `json:"base"`      // pacs.008
	Area      string `json:"area"`      // pacs
	Root      string `json:"root"`      // the business root element, FIToFICstmrCdtTrf
	Name      string `json:"name"`      // FI To FI Customer Credit Transfer
	Version   string `json:"version"`   // 08
	Latest    bool   `json:"latest"`    // the newest version of its message (of the main variant, 001, when it has one)
	Spoken    bool   `json:"spoken"`    // prova encodes and decodes it natively, beyond schema checks
}

// Areas names the ISO 20022 business areas by their four-letter code.
var Areas = map[string]string{
	"acmt": "Account management", "admi": "Administration", "auth": "Authorities", "caaa": "Acceptor to acquirer card transactions",
	"caad": "Card administration", "caam": "ATM management", "cafc": "Fee collection", "cafm": "File management",
	"cafr": "Fraud reporting and disposition", "cain": "Acquirer to issuer card transactions", "camt": "Cash management",
	"canm": "Network management", "casp": "Sale to POI card transactions", "casr": "Settlement reporting",
	"catm": "Terminal management", "catp": "ATM card transactions", "colr": "Collateral management", "fxtr": "Foreign exchange trade",
	"head": "Business application header", "pacs": "Payments clearing and settlement", "pain": "Payments initiation",
	"reda": "Reference data", "remt": "Payments remittance advice", "secl": "Securities clearing", "seev": "Securities events",
	"semt": "Securities management", "sese": "Securities settlement", "setr": "Securities trade", "tsin": "Trade services initiation",
	"tsmt": "Trade services management", "tsrv": "Trade services",
}

var (
	catOnce  sync.Once
	catList  []CatalogueMessage
	catByKey map[string]CatalogueMessage
	catByNS  map[string]string // target namespace -> kind
	catZip   *zip.Reader
	catFiles map[string]*zip.File
	catErr   error

	catMu    sync.Mutex
	catCache = map[string]*schema{} // parsed schemas by kind, most recent last in catOrder
	catOrder []string
)

// catCacheSize bounds how many catalogue schemas stay parsed; the largest take a few megabytes each.
const catCacheSize = 48

func loadCatalogue() {
	catByKey, catByNS = map[string]CatalogueMessage{}, map[string]string{}
	if err := json.Unmarshal(catalogueIndex, &catList); err != nil {
		catErr = fmt.Errorf("validate: catalogue index: %w", err)
		return
	}
	spoken := map[string]bool{}
	for _, k := range bundledKinds() {
		spoken[k] = true
	}
	for i := range catList {
		catList[i].Spoken = spoken[catList[i].Kind]
		catByKey[catList[i].Kind] = catList[i]
		catByNS[catList[i].Namespace] = catList[i].Kind
	}
	r, err := zip.NewReader(bytes.NewReader(catalogueZip), int64(len(catalogueZip)))
	if err != nil {
		catErr = fmt.Errorf("validate: catalogue: %w", err)
		return
	}
	catZip, catFiles = r, map[string]*zip.File{}
	for _, f := range r.File {
		catFiles[strings.TrimSuffix(f.Name, ".xsd")] = f
	}
}

// Catalogue lists every message version in the ISO 20022 catalogue, sorted by kind.
func Catalogue() []CatalogueMessage {
	catOnce.Do(loadCatalogue)
	return append([]CatalogueMessage(nil), catList...)
}

// LookupMessage returns the catalogue entry for a kind such as pacs.008.001.08. A kind without its
// version (pacs.008) names the latest version.
func LookupMessage(kind string) (CatalogueMessage, bool) {
	catOnce.Do(loadCatalogue)
	if m, ok := catByKey[kind]; ok {
		return m, true
	}
	var best CatalogueMessage
	for _, m := range catList {
		if m.Base == kind && m.Latest {
			best = m
		}
	}
	return best, best.Kind != ""
}

// KindForNamespace returns the message version a document namespace names, or "" when no schema in
// the catalogue has that namespace. Most are urn:iso:std:iso:20022:tech:xsd:KIND; some older ones are
// urn:iso:std:iso:20022:xsd:KIND or urn:swift:xsd:KIND.
func KindForNamespace(ns string) string {
	catOnce.Do(loadCatalogue)
	return catByNS[ns]
}

// CatalogueSchema returns the official XSD of one message version.
func CatalogueSchema(kind string) ([]byte, error) {
	catOnce.Do(loadCatalogue)
	if catErr != nil {
		return nil, catErr
	}
	if raw, err := schemaFS.ReadFile("xsd/" + kind + ".xsd"); err == nil {
		return raw, nil
	}
	f := catFiles[kind]
	if f == nil {
		return nil, fmt.Errorf("validate: %s is not in the ISO 20022 catalogue", kind)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// schemaForKind returns the parsed schema for a message version: one of the curated schemas prova
// speaks, or a catalogue schema parsed on first use and kept in a small cache.
func schemaForKind(kind string) (*schema, error) {
	schemasOnce.Do(loadSchemas)
	if schemasErr != nil {
		return nil, schemasErr
	}
	if s := schemas["urn:iso:std:iso:20022:tech:xsd:"+kind]; s != nil {
		return s, nil
	}
	catMu.Lock()
	if s := catCache[kind]; s != nil {
		catMu.Unlock()
		return s, nil
	}
	catMu.Unlock()
	raw, err := CatalogueSchema(kind)
	if err != nil {
		return nil, err
	}
	s, err := parseSchema(raw)
	if err != nil {
		return nil, fmt.Errorf("validate: %s: %w", kind, err)
	}
	catMu.Lock()
	defer catMu.Unlock()
	if _, ok := catCache[kind]; !ok {
		catCache[kind] = s
		catOrder = append(catOrder, kind)
		if len(catOrder) > catCacheSize {
			delete(catCache, catOrder[0])
			catOrder = catOrder[1:]
		}
	}
	return s, nil
}

// --- building the index (run by TestCatalogueIndex with PROVA_REGEN_CATALOGUE=1) -------------

var reKindName = regexp.MustCompile(`^([a-z]{4})\.(\d{3})\.(\d{3})\.(\d{2})$`)

// buildIndex parses every catalogue schema and lists its message. It is slow (every schema is
// parsed), so the result is committed as catalogue/index.json and checked by a test.
func buildIndex() ([]CatalogueMessage, error) {
	r, err := zip.NewReader(bytes.NewReader(catalogueZip), int64(len(catalogueZip)))
	if err != nil {
		return nil, err
	}
	var out []CatalogueMessage
	latest := map[string]string{} // by message: the highest version of the lowest variant
	rank := func(kind string) string {
		m := reKindName.FindStringSubmatch(kind)
		// the main variant (001) first, then the highest version
		return fmt.Sprintf("%03d-%s", 999-atoi(m[3]), m[4])
	}
	for _, f := range r.File {
		kind := strings.TrimSuffix(f.Name, ".xsd")
		m := reKindName.FindStringSubmatch(kind)
		if m == nil {
			return nil, fmt.Errorf("%s: not a message file name", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		s, err := parseSchema(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", kind, err)
		}
		root, typ := "", ""
		if ct := s.complexs[s.root]; ct != nil && len(ct.particles) > 0 {
			root, typ = ct.particles[0].name, ct.particles[0].typ
		} else {
			for name, t := range s.globals {
				root, typ = name, t
			}
		}
		base := m[1] + "." + m[2]
		out = append(out, CatalogueMessage{Kind: kind, Namespace: s.ns, Base: base, Area: m[1], Root: root, Name: readableName(typ), Version: m[4]})
		if latest[base] == "" || rank(kind) > rank(latest[base]) {
			latest[base] = kind
		}
	}
	for i := range out {
		out[i].Latest = latest[out[i].Base] == out[i].Kind
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

// readableName turns a type name such as FIToFICustomerCreditTransferV08 into "FI To FI Customer
// Credit Transfer".
func readableName(typ string) string {
	typ = regexp.MustCompile(`V\d+$`).ReplaceAllString(typ, "")
	var b strings.Builder
	rs := []rune(typ)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1])) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
