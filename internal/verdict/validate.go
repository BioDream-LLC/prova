package verdict

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Field validation: the checks a receiving host must make before it acts on an instruction. Each
// reads a canonical request key only when it is present, so the same checks apply to a pacs.008, an
// ISO 8583 authorisation and a FIX order alike. prova's second engine (internal/qcasebook) carries its
// own, separately written implementation of the same checks; the two are compared on every run.

var bicPattern = regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`)

// knownCurrencies is the ISO 4217 subset prova recognises, with each currency's minor units.
var knownCurrencies = map[string]int{
	"EUR": 2, "USD": 2, "GBP": 2, "CHF": 2, "CAD": 2, "AUD": 2, "NZD": 2, "SEK": 2, "NOK": 2, "DKK": 2,
	"PLN": 2, "CZK": 2, "HUF": 2, "SGD": 2, "HKD": 2, "CNY": 2, "INR": 2, "ZAR": 2, "MXN": 2, "BRL": 2,
	"TRY": 2, "JPY": 0, "KRW": 0, "BHD": 3, "KWD": 3, "OMR": 3, "JOD": 3, "TND": 3,
}

// FieldProblems lists every field-validation failure in a request, in a stable order. An empty
// result means the instruction is well formed as far as prova can tell from its fields.
func FieldProblems(req map[string]string) []string {
	var out []string
	ccy := strings.TrimSpace(req["Currency"])
	if ccy != "" {
		if _, ok := knownCurrencies[ccy]; !ok {
			out = append(out, "currency "+ccy+" is not an ISO 4217 code")
		}
	}
	if a, ok := req["Amount"]; ok && strings.TrimSpace(a) != "" {
		out = append(out, amountProblems(strings.TrimSpace(a), ccy)...)
	}
	for _, k := range []string{"DebtorBIC", "CreditBIC"} {
		if v := strings.TrimSpace(req[k]); v != "" && !bicPattern.MatchString(v) {
			out = append(out, k+" "+v+" is not a valid BIC")
		}
	}
	for _, k := range []string{"DebtorIBAN", "CreditIBAN"} {
		if v := strings.TrimSpace(req[k]); v != "" && !ibanValid(v) {
			out = append(out, k+" "+v+" fails the IBAN check")
		}
	}
	if v := strings.TrimSpace(req["PAN"]); v != "" && !panValid(v) {
		out = append(out, "card number fails the Luhn check")
	}
	if v, ok := req["OrderQty"]; ok {
		q := strings.TrimSpace(v)
		if n, err := strconv.ParseInt(q, 10, 64); err != nil || n <= 0 || len(strings.Map(keepDigits, q)) != len(q) || len(q) > 18 {
			out = append(out, "order quantity "+v+" is not a positive whole number")
		}
	}
	if v, ok := req["Price"]; ok {
		p := strings.TrimSpace(v)
		if r, ok := decimal(p); !ok || r.Sign() <= 0 || len(strings.Map(keepDigits, p)) > 18 {
			out = append(out, "price "+v+" is not a positive amount")
		}
	}
	out = append(out, cardSecurityProblems(req)...)
	return out
}

// cardSecurityProblems lists failures in the PIN block, MAC and EMV chip data of a card request: a
// PIN block that is not 16 hex, a MAC that is not 16 or 32 hex, malformed BER-TLV chip data or chip
// data missing its Application Cryptogram (9F26), and a chip entry carrying no EMV data at all.
func cardSecurityProblems(req map[string]string) []string {
	var out []string
	if p := strings.TrimSpace(req["PINBlock"]); p != "" && !hexOfLen(p, 16) {
		out = append(out, "PIN block is not a 16-hex ISO 9564 block")
	}
	if m := strings.TrimSpace(req["MAC"]); m != "" && !hexOfLen(m, 16) && !hexOfLen(m, 32) {
		out = append(out, "MAC is not 16 or 32 hex characters")
	}
	entry := strings.TrimSpace(req["POSEntryMode"])
	chip := strings.HasPrefix(entry, "05") || strings.HasPrefix(entry, "07")
	icc := strings.TrimSpace(req["ICCData"])
	switch {
	case icc == "":
		if chip {
			out = append(out, "chip entry ("+entry+") carries no EMV data (DE55)")
		}
	default:
		tags, ok := berTLVTags(icc)
		if !ok {
			out = append(out, "EMV chip data is not well-formed BER-TLV")
		} else if !tags["9F26"] {
			out = append(out, "EMV chip data carries no Application Cryptogram (tag 9F26)")
		}
	}
	return out
}

// hexOfLen reports whether s is exactly n hex characters.
func hexOfLen(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// berTLVTags parses BER-TLV encoded as hex and returns the set of tags, or ok=false if malformed.
// This is prova's Go-casebook implementation; the sim and the second engine carry their own.
func berTLVTags(h string) (map[string]bool, bool) {
	if len(h)%2 != 0 {
		return nil, false
	}
	buf := make([]byte, len(h)/2)
	for i := range buf {
		var hi, lo int
		if hi = hexVal(h[2*i]); hi < 0 {
			return nil, false
		}
		if lo = hexVal(h[2*i+1]); lo < 0 {
			return nil, false
		}
		buf[i] = byte(hi<<4 | lo)
	}
	tags := map[string]bool{}
	for i := 0; i < len(buf); {
		ts := i
		b0 := buf[i]
		i++
		if b0&0x1f == 0x1f {
			for i < len(buf) && buf[i]&0x80 != 0 {
				i++
			}
			if i >= len(buf) {
				return nil, false
			}
			i++
		}
		tag := strings.ToUpper(hexEncode(buf[ts:i]))
		if i >= len(buf) {
			return nil, false
		}
		n := int(buf[i])
		i++
		if n&0x80 != 0 {
			k := n & 0x7f
			if k == 0 || k > 2 || i+k > len(buf) {
				return nil, false
			}
			n = 0
			for ; k > 0; k-- {
				n = n<<8 | int(buf[i])
				i++
			}
		}
		if i+n > len(buf) {
			return nil, false
		}
		i += n
		tags[tag] = true
	}
	return tags, true
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}

func hexEncode(b []byte) string {
	const d = "0123456789ABCDEF"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, d[c>>4], d[c&0xf])
	}
	return string(out)
}

func amountProblems(a, ccy string) []string {
	r, ok := decimal(a)
	if !ok {
		return []string{"amount " + a + " is not a number"}
	}
	var out []string
	if r.Sign() <= 0 {
		out = append(out, "amount "+a+" is not positive")
	}
	_, frac, _ := strings.Cut(strings.TrimLeft(a, "+-"), ".")
	units := 2
	if u, ok := knownCurrencies[ccy]; ok {
		units = u
	}
	if len(frac) > units {
		out = append(out, "amount "+a+" has more decimals than "+orDefault(ccy, "the currency")+" allows ("+strconv.Itoa(units)+")")
	}
	if digits := len(strings.Map(keepDigits, a)); digits > 18 {
		out = append(out, "amount "+a+" has more than 18 digits")
	}
	return out
}

func keepDigits(r rune) rune {
	if r >= '0' && r <= '9' {
		return r
	}
	return -1
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// decimal parses a plain decimal string ("150.00", "-3", "0.5") exactly.
func decimal(s string) (*big.Rat, bool) {
	if s == "" || strings.ContainsAny(s, "eE/xXpP_") {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

// ibanValid applies the ISO 13616 checks: two letters, two check digits, an alphanumeric BBAN, a
// total length of 15 to 34, and the mod-97 remainder of 1 after moving the first four characters to
// the end and replacing each letter by its two-digit value.
func ibanValid(iban string) bool {
	s := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	for i, c := range s {
		switch {
		case i < 2 && (c < 'A' || c > 'Z'):
			return false
		case i >= 2 && i < 4 && (c < '0' || c > '9'):
			return false
		case !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z'):
			return false
		}
	}
	var num strings.Builder
	for _, c := range s[4:] + s[:4] {
		if c >= 'A' {
			num.WriteString(strconv.Itoa(int(c-'A') + 10))
		} else {
			num.WriteRune(c)
		}
	}
	n, ok := new(big.Int).SetString(num.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

// panValid checks a card number is 12 to 19 digits and satisfies the Luhn checksum.
func panValid(pan string) bool {
	if len(pan) < 12 || len(pan) > 19 {
		return false
	}
	sum := 0
	for i := 0; i < len(pan); i++ {
		c := pan[len(pan)-1-i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
