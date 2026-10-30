package qcasebook

import (
	"crypto/aes"
	"crypto/des"
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
)

// The second engine's own card-security verification, written separately from prova's cardcrypto
// package and by a different construction: explicit block-by-block chaining, PIN fields assembled as
// nibble strings, and its own TLV cursor. It shares only Go's block ciphers. The cross-engine tests
// compare the two on every case they generate.

// desKey3 expands a 16-byte key K1K2 to K1K2K1; 24-byte keys pass through.
func desKey3(k []byte) []byte {
	if len(k) == 16 {
		return append(append([]byte{}, k...), k[:8]...)
	}
	return k
}

func tdesEncBlock(k, in []byte) []byte {
	if len(in) != 8 {
		return nil
	}
	c, err := des.NewTripleDESCipher(desKey3(k))
	if err != nil {
		return nil
	}
	out := make([]byte, 8)
	c.Encrypt(out, in)
	return out
}

// tdesDecBlock deciphers one block under a single, double or triple length DES key.
func tdesDecBlock(k, in []byte) []byte {
	if len(in) != 8 {
		return nil
	}
	out := make([]byte, 8)
	if len(k) == 8 {
		c, err := des.NewCipher(k)
		if err != nil {
			return nil
		}
		c.Decrypt(out, in)
		return out
	}
	c, err := des.NewTripleDESCipher(desKey3(k))
	if err != nil {
		return nil
	}
	c.Decrypt(out, in)
	return out
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

func xorInto(dst, a []byte) {
	for i := range dst {
		dst[i] ^= a[i]
	}
}

func unhex(s string) []byte {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return b
}

func hexU(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

// pinOf recovers a PIN from an enciphered PIN block of ISO format 0, 1, 3 or 4, or "" if the block is
// not valid.
func pinOf(format string, key, block []byte, pan string) string {
	f := strings.TrimLeft(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(format)), "ISO"), "- ")
	if f == "" {
		f = "0"
	}
	switch f {
	case "4":
		return pinOf4(key, block, pan)
	case "0", "1", "3":
	default:
		return ""
	}
	if len(block) != 8 || (len(key) != 8 && len(key) != 16 && len(key) != 24) {
		return ""
	}
	clear := tdesDecBlock(key, block)
	if clear == nil {
		return ""
	}
	nib := hexU(clear)
	if f == "0" || f == "3" {
		if len(pan) < 13 || !allDigits(pan) {
			return ""
		}
		acct := "0000" + pan[len(pan)-13:len(pan)-1]
		var sb strings.Builder
		for i := 0; i < 16; i++ {
			a, _ := strconv.ParseUint(nib[i:i+1], 16, 8)
			b, _ := strconv.ParseUint(acct[i:i+1], 16, 8)
			sb.WriteString(strings.ToUpper(strconv.FormatUint(a^b, 16)))
		}
		nib = sb.String()
	}
	if nib[:1] != f {
		return ""
	}
	n, err := strconv.ParseUint(nib[1:2], 16, 8)
	if err != nil || n < 4 || n > 12 {
		return ""
	}
	pin := nib[2 : 2+n]
	for i := 0; i < len(pin); i++ {
		if !isDigit(pin[i]) {
			return ""
		}
	}
	for i := 2 + int(n); i < 16; i++ {
		c := nib[i]
		if f == "0" && c != 'F' || f == "3" && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return pin
}

func pinOf4(key, block []byte, pan string) string {
	c, err := aes.NewCipher(key)
	if err != nil || len(block) != 16 || len(pan) > 19 || !allDigits(pan) {
		return ""
	}
	m := len(pan) - 12
	if m < 0 {
		m = 0
	}
	p := strings.Repeat("0", max(0, 12-len(pan))) + pan
	panField := unhex((strconv.Itoa(m) + p + strings.Repeat("0", 32))[:32])
	b := make([]byte, 16)
	c.Decrypt(b, block)
	xorInto(b, panField)
	field := make([]byte, 16)
	c.Decrypt(field, b)
	nib := hexU(field)
	if nib[0] != '4' {
		return ""
	}
	n, err := strconv.ParseUint(nib[1:2], 16, 8)
	if err != nil || n < 4 || n > 12 {
		return ""
	}
	for i := 2; i < 2+int(n); i++ {
		if !isDigit(nib[i]) {
			return ""
		}
	}
	for i := 2 + int(n); i < 16; i++ {
		if nib[i] != 'A' {
			return ""
		}
	}
	return nib[2 : 2+n]
}

// retail is ISO 9797-1 algorithm 3 over data padded with zeros (method 1) or 0x80 (method 2), chained
// a block at a time.
func retail(key, data []byte, method2 bool) []byte {
	if len(key) != 16 {
		return nil
	}
	d := append([]byte{}, data...)
	if method2 {
		d = append(d, 0x80)
	}
	for len(d)%8 != 0 || len(d) == 0 {
		d = append(d, 0)
	}
	k1, err := des.NewCipher(key[:8])
	if err != nil {
		return nil
	}
	k2, _ := des.NewCipher(key[8:16])
	h := make([]byte, 8)
	for i := 0; i < len(d); i += 8 {
		xorInto(h, d[i:i+8])
		k1.Encrypt(h, h)
	}
	k2.Decrypt(h, h)
	k1.Encrypt(h, h)
	return h
}

func cbcmac(key, data []byte) []byte {
	d := append([]byte{}, data...)
	for len(d)%8 != 0 || len(d) == 0 {
		d = append(d, 0)
	}
	h := make([]byte, 8)
	for i := 0; i < len(d); i += 8 {
		xorInto(h, d[i:i+8])
		var nh []byte
		if len(key) == 8 {
			c, err := des.NewCipher(key)
			if err != nil {
				return nil
			}
			nh = make([]byte, 8)
			c.Encrypt(nh, h)
		} else {
			nh = tdesEncBlock(key, h)
		}
		if nh == nil {
			return nil
		}
		h = nh
	}
	return h
}

// cmacAES derives the CMAC subkeys by shifting the whole 128-bit value as two 64-bit halves.
func cmacAES(key, data []byte) []byte {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	shift := func(in []byte) []byte {
		hi := uint64(0)
		lo := uint64(0)
		for i := 0; i < 8; i++ {
			hi = hi<<8 | uint64(in[i])
			lo = lo<<8 | uint64(in[8+i])
		}
		msb := hi >> 63
		hi = hi<<1 | lo>>63
		lo <<= 1
		if msb == 1 {
			lo ^= 0x87
		}
		out := make([]byte, 16)
		for i := 7; i >= 0; i-- {
			out[i] = byte(hi)
			out[8+i] = byte(lo)
			hi >>= 8
			lo >>= 8
		}
		return out
	}
	l := make([]byte, 16)
	c.Encrypt(l, l)
	k1 := shift(l)
	k2 := shift(k1)
	blocks := len(data) / 16
	complete := len(data) > 0 && len(data)%16 == 0
	if !complete {
		blocks++
	}
	x := make([]byte, 16)
	for i := 0; i < blocks; i++ {
		blk := make([]byte, 16)
		copy(blk, data[i*16:min(len(data), (i+1)*16)])
		if i == blocks-1 {
			if complete {
				xorInto(blk, k1)
			} else {
				blk[len(data)-i*16] = 0x80
				xorInto(blk, k2)
			}
		}
		xorInto(x, blk)
		c.Encrypt(x, x)
	}
	return x
}

func macInput(r map[string]string) []byte {
	var b []byte
	for _, k := range []string{"PAN", "ProcessingCode", "Amount", "Currency", "STAN", "Terminal"} {
		v := r[k]
		switch k {
		case "Amount":
			v = centsForm(v)
		case "Currency":
			v = strings.ToUpper(v)
		}
		b = append(b, []byte(v)...)
		b = append(b, 0x1C)
	}
	return b
}

// centsForm writes a plain amount with two decimals by counting digits either side of the point.
func centsForm(a string) string {
	a = strings.TrimSpace(a)
	dot := strings.IndexByte(a, '.')
	whole, frac := a, ""
	if dot >= 0 {
		whole, frac = a[:dot], a[dot+1:]
		if frac == "" || len(frac) > 2 {
			return a
		}
	}
	if whole == "" || !allDigits(whole) || frac != "" && !allDigits(frac) {
		return a
	}
	for len(whole) > 0 && whole[0] == '0' {
		whole = whole[1:]
	}
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return whole + "." + frac
}

func macOK(alg string, key []byte, r map[string]string) bool {
	given := unhex(r["MAC"])
	if len(given) < 4 {
		return false
	}
	var want []byte
	switch strings.ToLower(strings.TrimSpace(alg)) {
	case "", "retail", "x9.19", "iso9797-3":
		want = retail(key, macInput(r), false)
	case "cbc", "iso9797-1":
		want = cbcmac(key, macInput(r))
	case "cmac", "aes-cmac":
		want = cmacAES(key, macInput(r))
	}
	return want != nil && len(given) <= len(want) && hexU(given) == hexU(want[:len(given)])
}

// tlvValues walks hex BER-TLV into a tag -> value map.
func tlvValues(h string) map[string][]byte {
	buf := unhex(h)
	if buf == nil {
		return nil
	}
	out := map[string][]byte{}
	for i := 0; i < len(buf); {
		start := i
		if buf[i]&0x1F == 0x1F {
			for i++; i < len(buf) && buf[i]&0x80 != 0; i++ {
			}
		}
		i++
		if i >= len(buf) {
			return nil
		}
		tag := hexU(buf[start:i])
		n := int(buf[i])
		i++
		if n > 0x80 {
			k := n - 0x80
			if k > 2 || i+k > len(buf) {
				return nil
			}
			n = 0
			for j := 0; j < k; j++ {
				n = n<<8 | int(buf[i+j])
			}
			i += k
		} else if n == 0x80 {
			return nil
		}
		if i+n > len(buf) {
			return nil
		}
		out[tag] = buf[i : i+n]
		i += n
	}
	return out
}

func iccMasterKey(imk []byte, pan, psn string, optionB bool) []byte {
	if len(imk) != 16 || !allDigits(pan) || !allDigits(psn) {
		return nil
	}
	digits := pan + psn
	if optionB && len(pan) > 16 {
		raw := digits
		if len(raw)%2 == 1 {
			raw = "0" + raw
		}
		sum := sha1.Sum(unhex(raw))
		h := hex.EncodeToString(sum[:])
		var dec []byte
		for i := 0; i < len(h) && len(dec) < 16; i++ {
			if isDigit(h[i]) {
				dec = append(dec, h[i])
			}
		}
		for i := 0; i < len(h) && len(dec) < 16; i++ {
			if h[i] >= 'a' {
				dec = append(dec, '0'+h[i]-'a')
			}
		}
		digits = string(dec)
	} else {
		if len(digits) > 16 {
			digits = digits[len(digits)-16:]
		}
		digits = strings.Repeat("0", 16-len(digits)) + digits
	}
	a := unhex(digits)
	b := make([]byte, 8)
	for i := range b {
		b[i] = ^a[i]
	}
	l, r := tdesEncBlock(imk, a), tdesEncBlock(imk, b)
	if l == nil || r == nil {
		return nil
	}
	return oddParity(append(l, r...))
}

func oddParity(k []byte) []byte {
	for i, v := range k {
		p := v >> 1
		p ^= p >> 4
		p ^= p >> 2
		p ^= p >> 1
		k[i] = v&0xFE | (^p & 1)
	}
	return k
}

func sessionKey(mk, r []byte) []byte {
	if mk == nil || len(r) != 8 {
		return nil
	}
	a := append([]byte{}, r...)
	b := append([]byte{}, r...)
	a[2], b[2] = 0xF0, 0x0F
	l, rr := tdesEncBlock(mk, a), tdesEncBlock(mk, b)
	if l == nil || rr == nil {
		return nil
	}
	return oddParity(append(l, rr...))
}

func arqcOK(version string, imk []byte, pan, psn, icc string) bool {
	t := tlvValues(icc)
	if t == nil || t["9F26"] == nil {
		return false
	}
	if v := t["5F34"]; len(v) == 1 {
		psn = hexU(v)
	}
	if psn == "" {
		psn = "00"
	}
	var data []byte
	for _, tag := range []string{"9F02", "9F03", "9F1A", "95", "5F2A", "9A", "9C", "9F37", "82", "9F36"} {
		v, ok := t[tag]
		if !ok {
			return false
		}
		data = append(data, v...)
	}
	iad := t["9F10"]
	var ac []byte
	switch strings.TrimSpace(version) {
	case "", "visa-cvn10":
		if len(iad) < 7 {
			return false
		}
		ac = retail(iccMasterKey(imk, pan, psn, false), append(data, iad[3:7]...), false)
	case "visa-cvn18":
		if len(iad) == 0 || len(t["9F36"]) != 2 {
			return false
		}
		r := make([]byte, 8)
		copy(r, t["9F36"])
		ac = retail(sessionKey(iccMasterKey(imk, pan, psn, true), r), append(data, iad...), true)
	case "mc-cvn16":
		if len(iad) < 8 || len(t["9F37"]) != 4 || len(t["9F36"]) != 2 {
			return false
		}
		r := append(append(append([]byte{}, t["9F36"]...), 0, 0), t["9F37"]...)
		ac = retail(sessionKey(iccMasterKey(imk, pan, psn, false), r), append(data, iad[2:8]...), true)
	default:
		return false
	}
	return ac != nil && hexU(ac) == hexU(t["9F26"])
}

// securityFails is the binding layer's card-security reading: under the seeded test keys, the PIN
// block, MAC or chip cryptogram does not verify.
func securityFails(r, ctx map[string]string) bool {
	get := func(k string) string { return strings.TrimSpace(ctx[k]) }
	pan := strings.TrimSpace(r["PAN"])
	if key, pin, block := get("pinKey"), get("cardPIN"), strings.TrimSpace(r["PINBlock"]); key != "" && pin != "" && block != "" {
		k, b := unhex(key), unhex(block)
		if k == nil || b == nil || pinOf(get("pinFormat"), k, b, pan) != pin {
			return true
		}
	}
	if key, mac := get("macKey"), strings.TrimSpace(r["MAC"]); key != "" && mac != "" {
		k := unhex(key)
		if k == nil || !macOK(get("macAlgorithm"), k, r) {
			return true
		}
	}
	if imk, icc := get("imk"), strings.TrimSpace(r["ICCData"]); imk != "" && icc != "" {
		k := unhex(imk)
		if k == nil || !arqcOK(get("cryptogramVersion"), k, pan, get("psn"), icc) {
			return true
		}
	}
	return false
}
