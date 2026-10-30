package cardcrypto

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"
)

// TLV is one BER-TLV data object of EMV chip data.
type TLV struct {
	Tag   string // upper-case hex, e.g. "9F26"
	Value []byte
}

// ParseTLV decodes hex BER-TLV (one level: EMV DE55 carries primitive objects) into its objects, in
// order. Lengths of one, two and three bytes are accepted.
func ParseTLV(h string) ([]TLV, error) {
	buf, err := Hex(h)
	if err != nil {
		return nil, err
	}
	var out []TLV
	for i := 0; i < len(buf); {
		start := i
		if buf[i]&0x1F == 0x1F {
			i++
			for i < len(buf) && buf[i]&0x80 != 0 {
				i++
			}
		}
		i++
		if i > len(buf) {
			return nil, fmt.Errorf("cardcrypto: TLV tag runs past the end")
		}
		tag := HexUpper(buf[start:i])
		if i >= len(buf) {
			return nil, fmt.Errorf("cardcrypto: TLV %s has no length", tag)
		}
		n := int(buf[i])
		i++
		if n&0x80 != 0 {
			k := n & 0x7F
			if k == 0 || k > 2 || i+k > len(buf) {
				return nil, fmt.Errorf("cardcrypto: TLV %s has a bad length", tag)
			}
			n = 0
			for ; k > 0; k-- {
				n = n<<8 | int(buf[i])
				i++
			}
		}
		if i+n > len(buf) {
			return nil, fmt.Errorf("cardcrypto: TLV %s value runs past the end", tag)
		}
		out = append(out, TLV{Tag: tag, Value: append([]byte(nil), buf[i:i+n]...)})
		i += n
	}
	return out, nil
}

// BuildTLV encodes objects as hex BER-TLV.
func BuildTLV(objs []TLV) string {
	var b strings.Builder
	for _, o := range objs {
		b.WriteString(o.Tag)
		n := len(o.Value)
		switch {
		case n < 0x80:
			fmt.Fprintf(&b, "%02X", n)
		case n < 0x100:
			fmt.Fprintf(&b, "81%02X", n)
		default:
			fmt.Fprintf(&b, "82%04X", n)
		}
		b.WriteString(HexUpper(o.Value))
	}
	return b.String()
}

func tagMap(objs []TLV) map[string][]byte {
	m := map[string][]byte{}
	for _, o := range objs {
		m[o.Tag] = o.Value
	}
	return m
}

// Cryptogram versions prova computes.
const (
	VisaCVN10 = "visa-cvn10" // MK option A; no session key; Visa (zero) padding; CVR from IAD bytes 4-7
	VisaCVN18 = "visa-cvn18" // MK option B; EMV common session key on the ATC; EMV padding; whole IAD
	MCCVN16   = "mc-cvn16"   // MK option A; Mastercard session key on ATC and UN; EMV padding; CVR from IAD bytes 3-8
)

// CryptogramVersions lists the versions prova supports, for the interface.
func CryptogramVersions() []string { return []string{VisaCVN10, VisaCVN18, MCCVN16} }

// MasterKeyA is EMV Book 2 Annex A1.4.1 option A: Triple DES of the rightmost 16 digits of PAN||PSN and
// their complement under the issuer master key.
func MasterKeyA(imk []byte, pan, psn string) ([]byte, error) {
	if psn == "" {
		psn = "00"
	}
	d := pan + psn
	if len(d) > 16 {
		d = d[len(d)-16:]
	}
	d = strings.Repeat("0", 16-len(d)) + d
	a, err := Hex(d)
	if err != nil {
		return nil, err
	}
	b := xor(a, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	mk, err := EncryptTDES(imk, append(a, b...))
	if err != nil {
		return nil, err
	}
	return adjustParity(mk), nil
}

// MasterKeyB is EMV option B: option A for a PAN of 16 digits or fewer; otherwise SHA-1 of PAN||PSN,
// decimalised (digits first, then the letters a-f as 0-5) to 16 digits.
func MasterKeyB(imk []byte, pan, psn string) ([]byte, error) {
	if len(pan) <= 16 {
		return MasterKeyA(imk, pan, psn)
	}
	if psn == "" {
		psn = "00"
	}
	d := pan + psn
	if len(d)%2 == 1 {
		d = "0" + d
	}
	raw, err := Hex(d)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(raw)
	hx := strings.ToLower(HexUpper(sum[:]))
	var res strings.Builder
	for _, c := range hx {
		if c >= '0' && c <= '9' && res.Len() < 16 {
			res.WriteRune(c)
		}
	}
	for _, c := range hx {
		if c >= 'a' && c <= 'f' && res.Len() < 16 {
			res.WriteByte(byte('0' + (c - 'a')))
		}
	}
	a, _ := Hex(res.String())
	b := xor(a, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	mk, err := EncryptTDES(imk, append(a, b...))
	if err != nil {
		return nil, err
	}
	return adjustParity(mk), nil
}

// CommonSessionKey is EMV Book 2 Annex A1.3.1: the left half from R with byte 3 set to F0, the right
// from R with byte 3 set to 0F, each enciphered under the ICC master key.
func CommonSessionKey(mk, r []byte) ([]byte, error) {
	if len(r) != 8 {
		return nil, fmt.Errorf("cardcrypto: diversification value must be 8 bytes")
	}
	ra := append([]byte(nil), r...)
	rb := append([]byte(nil), r...)
	ra[2], rb[2] = 0xF0, 0x0F
	sk, err := EncryptTDES(mk, append(ra, rb...))
	if err != nil {
		return nil, err
	}
	return adjustParity(sk), nil
}

// cdol is the cryptogram input common to the versions: amount, other amount, terminal country, TVR,
// currency, date, type, unpredictable number, AIP and ATC, in that order.
var cdol = []string{"9F02", "9F03", "9F1A", "95", "5F2A", "9A", "9C", "9F37", "82", "9F36"}

// Cryptogram computes the application cryptogram (ARQC, TC or AAC) a card of the given version
// generates over the chip data tags, from the issuer master key.
func Cryptogram(version string, imk []byte, pan, psn string, tags map[string][]byte) ([]byte, error) {
	if len(imk) != 16 {
		return nil, fmt.Errorf("cardcrypto: an issuer master key is a double-length key")
	}
	if psn == "" {
		psn = "00"
	}
	if !digitsOnly(pan) || !digitsOnly(psn) {
		return nil, fmt.Errorf("cardcrypto: PAN and PSN must be digits")
	}
	var data []byte
	for _, t := range cdol {
		v, ok := tags[t]
		if !ok {
			return nil, fmt.Errorf("cardcrypto: chip data lacks tag %s", t)
		}
		data = append(data, v...)
	}
	iad := tags["9F10"]
	atc := tags["9F36"]
	switch version {
	case VisaCVN10:
		if len(iad) < 7 {
			return nil, fmt.Errorf("cardcrypto: Visa CVN 10 needs the CVR in tag 9F10")
		}
		mk, err := MasterKeyA(imk, pan, psn)
		if err != nil {
			return nil, err
		}
		return RetailMAC(mk, PadISO1(append(data, iad[3:7]...), 8))
	case VisaCVN18:
		if len(iad) == 0 {
			return nil, fmt.Errorf("cardcrypto: Visa CVN 18 needs tag 9F10")
		}
		mk, err := MasterKeyB(imk, pan, psn)
		if err != nil {
			return nil, err
		}
		sk, err := CommonSessionKey(mk, append(append([]byte(nil), atc...), 0, 0, 0, 0, 0, 0))
		if err != nil {
			return nil, err
		}
		return RetailMAC(sk, PadISO2(append(data, iad...), 8))
	case MCCVN16:
		if len(iad) < 8 {
			return nil, fmt.Errorf("cardcrypto: Mastercard CVN 16 needs the CVR in tag 9F10")
		}
		mk, err := MasterKeyA(imk, pan, psn)
		if err != nil {
			return nil, err
		}
		r := append(append(append([]byte(nil), atc...), 0, 0), tags["9F37"]...)
		sk, err := CommonSessionKey(mk, r)
		if err != nil {
			return nil, err
		}
		return RetailMAC(sk, PadISO2(append(data, iad[2:8]...), 8))
	}
	return nil, fmt.Errorf("cardcrypto: cryptogram version %q is not one of %s", version, strings.Join(CryptogramVersions(), ", "))
}

// VerifyARQC reports whether the cryptogram in the chip data (tag 9F26) is the one the card would
// generate. The PAN sequence number comes from tag 5F34 when present, else psn.
func VerifyARQC(version string, imk []byte, pan, psn, iccHex string) (bool, error) {
	objs, err := ParseTLV(iccHex)
	if err != nil {
		return false, err
	}
	tags := tagMap(objs)
	ac, ok := tags["9F26"]
	if !ok {
		return false, fmt.Errorf("cardcrypto: chip data carries no cryptogram (9F26)")
	}
	if v, ok := tags["5F34"]; ok && len(v) == 1 {
		psn = fmt.Sprintf("%02X", v[0])
	}
	want, err := Cryptogram(version, imk, pan, psn, tags)
	if err != nil {
		return false, err
	}
	return HexUpper(want) == HexUpper(ac), nil
}

// ChipData describes a chip transaction for BuildChipData.
type ChipData struct {
	Version  string
	IMK      []byte
	PAN, PSN string
	Amount   int64  // minor units
	Currency string // ISO 4217 numeric, e.g. "978"
	Country  string // ISO 3166 numeric, e.g. "276"
	Date     string // YYMMDD
	UN       string // unpredictable number, 8 hex
	ATC      string // application transaction counter, 4 hex
}

// defaultIAD is a plausible issuer application data block per version (the CVR prova's verifier reads).
func defaultIAD(version string) string {
	switch version {
	case VisaCVN18:
		return "06011203A00000"
	case MCCVN16:
		return "0110A00003220000000000000000000000FF"
	}
	return "06010A03A00000"
}

// BuildChipData builds the DE55 chip data a card of the given version would send, with a correct
// ARQC (9F26) computed from the issuer master key.
func BuildChipData(c ChipData) (string, error) {
	n := func(v string, width int) []byte {
		if len(v) < width {
			v = strings.Repeat("0", width-len(v)) + v
		}
		b, _ := Hex(v)
		return b
	}
	psn := c.PSN
	if psn == "" {
		psn = "00"
	}
	objs := []TLV{
		{"9F02", n(fmt.Sprintf("%d", c.Amount), 12)},
		{"9F03", n("0", 12)},
		{"9F1A", n(c.Country, 4)},
		{"95", n("0000008000", 10)},
		{"5F2A", n(c.Currency, 4)},
		{"9A", n(c.Date, 6)},
		{"9C", n("00", 2)},
		{"9F37", n(c.UN, 8)},
		{"82", n("5800", 4)},
		{"9F36", n(c.ATC, 4)},
		{"9F10", n(defaultIAD(c.Version), len(defaultIAD(c.Version)))},
		{"5F34", n(psn, 2)},
		{"9F27", []byte{0x80}}, // ARQC
	}
	ac, err := Cryptogram(c.Version, c.IMK, c.PAN, psn, tagMap(objs))
	if err != nil {
		return "", err
	}
	objs = append([]TLV{{"9F26", ac}}, objs...)
	return BuildTLV(objs), nil
}

// SortedTags lists the tags present in hex chip data, for display.
func SortedTags(iccHex string) []string {
	objs, err := ParseTLV(iccHex)
	if err != nil {
		return nil
	}
	var out []string
	for _, o := range objs {
		out = append(out, o.Tag)
	}
	sort.Strings(out)
	return out
}
