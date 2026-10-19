package iso8583

import "strings"

// This file is the card-security checks a compliant switch makes on the chip, PIN and MAC data of a
// card-present transaction. prova's casebook (internal/verdict) and its second engine
// (internal/qcasebook) each carry their own, separately written implementation of the same checks;
// the sim's copy here decides the DE39 response code so scenarios run end to end.

// cardSecurityOK reports whether the PIN block (DE52), MAC (DE64) and EMV chip data (DE55) of a
// request are well formed, and that a chip or contactless-chip entry carries an ARQC. A field that is
// absent is not checked — only what is present, plus the chip-entry requirement.
func cardSecurityOK(f map[int]string) bool {
	if p := f[52]; p != "" && !isHexLen(p, 16) {
		return false
	}
	if m := f[64]; m != "" && !isHexLen(m, 16) && !isHexLen(m, 32) {
		return false
	}
	chip := strings.HasPrefix(f[22], "05") || strings.HasPrefix(f[22], "07")
	icc := f[55]
	if icc == "" {
		return !chip // a chip entry must carry EMV data
	}
	tags, ok := tlvTags(icc)
	if !ok {
		return false
	}
	return tags["9F26"] // the Application Cryptogram (ARQC) must be present
}

func isHexLen(s string, n int) bool {
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

// tlvTags parses BER-TLV encoded as hex and returns the set of tags present. It returns ok=false if
// the encoding is malformed (odd hex, a length that overruns, or trailing bytes). It handles
// multi-byte tags (first byte low 5 bits all set) and multi-byte lengths (0x81/0x82).
func tlvTags(hexStr string) (map[string]bool, bool) {
	b, ok := hexBytes(hexStr)
	if !ok {
		return nil, false
	}
	tags := map[string]bool{}
	i := 0
	for i < len(b) {
		// tag
		start := i
		if i >= len(b) {
			return nil, false
		}
		first := b[i]
		i++
		if first&0x1f == 0x1f { // multi-byte tag
			for i < len(b) && b[i]&0x80 != 0 {
				i++
			}
			if i >= len(b) {
				return nil, false
			}
			i++ // last tag byte
		}
		tag := strings.ToUpper(hexOf(b[start:i]))
		// length
		if i >= len(b) {
			return nil, false
		}
		l := int(b[i])
		i++
		if l&0x80 != 0 {
			n := l & 0x7f
			if n == 0 || n > 2 || i+n > len(b) {
				return nil, false
			}
			l = 0
			for k := 0; k < n; k++ {
				l = l<<8 | int(b[i])
				i++
			}
		}
		if i+l > len(b) {
			return nil, false
		}
		i += l
		tags[tag] = true
	}
	return tags, true
}

func hexBytes(s string) ([]byte, bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexNibble(s[2*i])
		lo, ok2 := hexNibble(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, false
		}
		out[i] = hi<<4 | lo
	}
	return out, true
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

const hexDigits = "0123456789ABCDEF"

func hexOf(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteByte(hexDigits[c>>4])
		sb.WriteByte(hexDigits[c&0x0f])
	}
	return sb.String()
}
