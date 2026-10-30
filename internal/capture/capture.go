// Package capture turns traffic captured from a production or test system into recordings prova can
// replay live against a target: FIX logs, ISO 8583 dumps and ISO 20022 files. It keeps the requests a
// tester sends (orders, authorisations, payment instructions) and drops the rest. With masking, card
// numbers, IBANs, account ids and names are replaced by stable stand-ins before anything is stored, so a
// capture can be replayed without carrying cardholder data into the test harness.
package capture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/iso8583"
)

// Options control an import.
type Options struct {
	Dialect string            // ISO 8583 wire dialect of the dump (generic, visa, mastercard)
	Mask    bool              // replace card numbers, IBANs, accounts and names
	Context map[string]string // seeded state applied to every recording (what the tester knows)
}

// Report says what an import kept and dropped.
type Report struct {
	Read    int            `json:"read"`    // messages found
	Kept    int            `json:"kept"`    // requests turned into recordings
	Skipped int            `json:"skipped"` // replies, session messages and messages prova does not send
	Kinds   map[string]int `json:"kinds"`
	Masked  bool           `json:"masked"`
	Notes   []string       `json:"notes,omitempty"`
}

// maxCapture caps one import.
const maxCapture = 64 << 20

// Import reads a capture of one protocol and returns its requests as recordings, in capture order.
func Import(protocol string, r io.Reader, opt Options) ([]engine.Recording, Report, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCapture+1))
	if err != nil {
		return nil, Report{}, err
	}
	if len(data) > maxCapture {
		return nil, Report{}, fmt.Errorf("capture: larger than %d MB", maxCapture>>20)
	}
	rep := Report{Kinds: map[string]int{}, Masked: opt.Mask}
	var recs []engine.Recording
	add := func(kind string, at time.Time, req map[string]string) {
		if opt.Mask {
			mask(req)
		}
		ctx := map[string]string{}
		for k, v := range opt.Context {
			ctx[k] = v
		}
		rep.Kept++
		rep.Kinds[kind]++
		recs = append(recs, engine.Recording{ScenarioID: fmt.Sprintf("capture-%d", rep.Kept), Scenario: "captured " + kind,
			Protocol: protocol, Message: kind, Request: req, Context: ctx, At: at})
	}
	switch protocol {
	case "fix":
		err = importFIX(data, &rep, add)
	case "iso8583":
		err = importISO8583(data, opt.Dialect, &rep, add)
	case "iso20022":
		err = importISO20022(data, &rep, add)
	default:
		return nil, rep, fmt.Errorf("capture: protocol must be fix, iso8583 or iso20022, not %q", protocol)
	}
	if err != nil {
		return nil, rep, err
	}
	if opt.Mask {
		rep.Notes = append(rep.Notes, "masking replaces card numbers, so PIN blocks, MACs and chip cryptograms bound to them no longer verify; seed no keys for masked traffic")
	}
	if rep.Kept == 0 {
		return nil, rep, fmt.Errorf("capture: no %s requests found in %d messages", protocol, rep.Read)
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].At.Before(recs[j].At) })
	return recs, rep, nil
}

// --- FIX ----------------------------------------------------------------------------------------

var (
	reFIXMsg   = regexp.MustCompile(`8=FIX[^\x01|^]*[\x01|^].*?(?:[\x01|^])10=\d{3}[\x01|^]?`)
	fixRequest = map[string]bool{"D": true, "F": true, "G": true, "H": true, "q": true}
)

// importFIX finds every FIX message in a log (QuickFIX and most engines write one per line, with SOH,
// | or ^A as the separator) and keeps the application requests.
func importFIX(data []byte, rep *Report, add func(string, time.Time, map[string]string)) error {
	text := strings.ReplaceAll(string(data), "^A", "\x01")
	for _, m := range reFIXMsg.FindAllString(text, -1) {
		rep.Read++
		f := map[int]string{}
		for _, p := range strings.FieldsFunc(m, func(r rune) bool { return r == 0x01 || r == '|' }) {
			k, v, ok := strings.Cut(p, "=")
			if !ok {
				continue
			}
			if tag, err := strconv.Atoi(k); err == nil {
				f[tag] = v
			}
		}
		mt := f[35]
		if !fixRequest[mt] {
			rep.Skipped++
			continue
		}
		req := map[string]string{}
		set := func(k string, tag int) {
			if v := f[tag]; v != "" {
				req[k] = v
			}
		}
		set("ClOrdID", 11)
		set("OrigClOrdID", 41)
		set("Account", 1)
		set("Symbol", 55)
		set("OrderQty", 38)
		set("Price", 44)
		set("Currency", 15)
		switch f[54] {
		case "1":
			req["Side"] = "buy"
		case "2":
			req["Side"] = "sell"
		}
		if mt == "q" && req["Symbol"] == "" {
			req["Symbol"] = "*"
		}
		if q, p := req["OrderQty"], req["Price"]; q != "" && p != "" {
			req["Amount"] = notional(p, q)
		}
		at, _ := time.Parse("20060102-15:04:05.000", f[52])
		if at.IsZero() {
			at, _ = time.Parse("20060102-15:04:05", f[52])
		}
		add(mt, at, req)
	}
	return nil
}

func notional(price, qty string) string {
	p, ok1 := new(big.Rat).SetString(price)
	q, ok2 := new(big.Rat).SetString(qty)
	if !ok1 || !ok2 {
		return ""
	}
	return new(big.Rat).Mul(p, q).FloatString(2)
}

// --- ISO 8583 -----------------------------------------------------------------------------------

var reHex = regexp.MustCompile(`^[0-9A-Fa-f\s]+$`)

// importISO8583 reads one message per line: hex (any dialect) or, for the generic dialect, the ASCII
// message itself. Requests (0100, 0200, 0400, 0800) are kept.
func importISO8583(data []byte, dialect string, rep *Report, add func(string, time.Time, map[string]string)) error {
	d, err := iso8583.DialectByName(dialect)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	year := time.Now().UTC().Year()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rep.Read++
		raw := []byte(line)
		if reHex.MatchString(line) && (d.Name != "generic" || !strings.HasPrefix(line, "0")) {
			b, err := hex.DecodeString(strings.Join(strings.Fields(line), ""))
			if err != nil {
				return fmt.Errorf("capture: line %d is not hex: %w", rep.Read, err)
			}
			raw = b
		}
		if len(raw) > 2 && d.Name != "generic" && int(binary.BigEndian.Uint16(raw[:2])) == len(raw)-2 {
			raw = raw[2:] // a dump that kept the two-byte frame length
		}
		mti, f, err := d.Unpack(raw)
		if err != nil {
			return fmt.Errorf("capture: message %d: %w", rep.Read, err)
		}
		switch mti {
		case "0100", "0200", "0400", "0800":
		default:
			rep.Skipped++
			continue
		}
		req := map[string]string{}
		set := func(k string, de int) {
			if v := f[de]; v != "" {
				req[k] = v
			}
		}
		set("PAN", 2)
		set("ProcessingCode", 3)
		set("STAN", 11)
		set("Terminal", 41)
		set("Expiry", 14)
		set("POSEntryMode", 22)
		set("PINBlock", 52)
		set("ICCData", 55)
		set("MAC", 64)
		set("NetworkCode", 70)
		if v := f[4]; v != "" {
			n, _ := strconv.ParseInt(strings.TrimLeft(v, "0"), 10, 64)
			req["Amount"] = fmt.Sprintf("%d.%02d", n/100, n%100)
		}
		if v := f[49]; v != "" {
			req["Currency"] = currencyAlpha(v)
		}
		if v := f[90]; len(v) >= 10 {
			req["OriginalSTAN"] = v[4:10]
		}
		var at time.Time
		if v := f[7]; len(v) == 10 {
			at, _ = time.Parse("2006"+"0102150405", strconv.Itoa(year)+v)
		}
		add(mti, at, req)
	}
	return sc.Err()
}

var numericCurrency = map[string]string{"978": "EUR", "840": "USD", "826": "GBP", "392": "JPY", "756": "CHF", "124": "CAD"}

func currencyAlpha(n string) string {
	if a, ok := numericCurrency[n]; ok {
		return a
	}
	return n
}

// --- ISO 20022 ----------------------------------------------------------------------------------

var reCreDtTm = regexp.MustCompile(`<CreDtTm>([^<]+)</CreDtTm>`)

// importISO20022 splits concatenated XML documents (or one per file, joined) and keeps the
// instructions prova sends.
func importISO20022(data []byte, rep *Report, add func(string, time.Time, map[string]string)) error {
	text := string(data)
	parts := regexp.MustCompile(`(?s)<Document\b.*?</Document>`).FindAllString(text, -1)
	sendable := map[string]bool{}
	for _, k := range iso20022.RequestKinds() {
		sendable[k] = true
	}
	for _, p := range parts {
		rep.Read++
		raw := []byte(p)
		kind := iso20022.Kind(raw)
		if !sendable[kind] {
			rep.Skipped++
			continue
		}
		r, err := iso20022.ParseRequest(raw)
		if err != nil {
			return fmt.Errorf("capture: document %d: %w", rep.Read, err)
		}
		var at time.Time
		if m := reCreDtTm.FindStringSubmatch(p); m != nil {
			at, _ = time.Parse(time.RFC3339, strings.TrimSpace(m[1]))
		}
		add(kind, at, r.Record())
	}
	return nil
}

// --- masking --------------------------------------------------------------------------------------

// mask replaces identifying values with stand-ins derived from a hash of the original, so the same
// card or account masks the same way across a capture (duplicates still look like duplicates).
func mask(req map[string]string) {
	for _, k := range []string{"PAN"} {
		if v := req[k]; v != "" {
			req[k] = maskPAN(v)
		}
	}
	for _, k := range []string{"DebtorIBAN", "CreditIBAN"} {
		if v := req[k]; v != "" {
			req[k] = maskIBAN(v)
		}
	}
	for _, k := range []string{"DebtorAcct", "CreditAcct", "Account"} {
		if v := req[k]; v != "" {
			if strings.EqualFold(v, req["DebtorIBAN"]) || strings.EqualFold(v, req["CreditIBAN"]) || ibanLike(v) {
				req[k] = maskIBAN(v)
			} else {
				req[k] = "ACC-" + digest(v, 8)
			}
		}
	}
	for _, k := range []string{"DebtorName", "CreditorName"} {
		if v := req[k]; v != "" {
			req[k] = "PARTY " + digest(v, 6)
		}
	}
}

func digest(s string, n int) string {
	h := sha256.Sum256([]byte("prova-mask:" + s))
	return strings.ToUpper(hex.EncodeToString(h[:]))[:n]
}

func digits(s string, n int) string {
	h := sha256.Sum256([]byte("prova-mask-digits:" + s))
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteByte('0' + h[i%len(h)]%10)
		if i%len(h) == len(h)-1 {
			h = sha256.Sum256(h[:])
		}
	}
	return b.String()
}

// maskPAN keeps the first six digits (the issuer's BIN, which routing needs) and replaces the rest,
// with a fresh Luhn check digit.
func maskPAN(pan string) string {
	if len(pan) < 12 {
		return pan
	}
	body := pan[:6] + digits(pan, len(pan)-7)
	sum := 0
	for i := 0; i < len(body); i++ {
		d := int(body[len(body)-1-i] - '0')
		if i%2 == 0 {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return body + strconv.Itoa((10-sum%10)%10)
}

func ibanLike(s string) bool {
	return len(s) >= 15 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' && s[2] >= '0' && s[2] <= '9'
}

// maskIBAN keeps the country and replaces the BBAN with digits of the same length, recomputing the
// check digits so the result is a valid IBAN.
func maskIBAN(iban string) string {
	s := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if !ibanLike(s) {
		return "ACC-" + digest(s, 8)
	}
	cc := s[:2]
	bban := digits(s, len(s)-4)
	num := bban + strconv.Itoa(int(cc[0]-'A'+10)) + strconv.Itoa(int(cc[1]-'A'+10)) + "00"
	n, _ := new(big.Int).SetString(num, 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	return fmt.Sprintf("%s%02d%s", cc, check, bban)
}
