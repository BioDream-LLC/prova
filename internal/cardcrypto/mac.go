package cardcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"fmt"
	"strings"
)

// MAC algorithms prova computes.
const (
	MACRetail = "retail" // ISO 9797-1 algorithm 3 (ANSI X9.19): single-DES CBC with K1, last block K2^-1 then K1
	MACCBC    = "cbc"    // ISO 9797-1 algorithm 1: CBC-MAC under the whole (Triple) DES key
	MACCMAC   = "cmac"   // AES-CMAC (NIST SP 800-38B, RFC 4493)
)

// PadISO1 is ISO 9797-1 padding method 1: zeros to a whole block (none when already whole; one zero
// block for empty data).
func PadISO1(data []byte, bs int) []byte {
	if len(data) > 0 && len(data)%bs == 0 {
		return append([]byte(nil), data...)
	}
	return append(append([]byte(nil), data...), make([]byte, bs-len(data)%bs)...)
}

// PadISO2 is ISO 9797-1 padding method 2: 0x80, then zeros to a whole block (always added).
func PadISO2(data []byte, bs int) []byte {
	out := append(append([]byte(nil), data...), 0x80)
	if r := len(out) % bs; r != 0 {
		out = append(out, make([]byte, bs-r)...)
	}
	return out
}

func cbcLast(b cipher.Block, data []byte) []byte {
	iv := make([]byte, b.BlockSize())
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, data)
	return out[len(out)-b.BlockSize():]
}

// RetailMAC is ISO 9797-1 MAC algorithm 3 with a double-length key K1K2 over already padded data.
func RetailMAC(key, padded []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("cardcrypto: the retail MAC needs a double-length key")
	}
	if len(padded)%8 != 0 || len(padded) == 0 {
		return nil, fmt.Errorf("cardcrypto: MAC data must be padded to whole blocks")
	}
	k1, err := des.NewCipher(key[:8])
	if err != nil {
		return nil, err
	}
	k2, err := des.NewCipher(key[8:])
	if err != nil {
		return nil, err
	}
	h := cbcLast(k1, padded)
	k2.Decrypt(h, h)
	k1.Encrypt(h, h)
	return h, nil
}

// CBCMAC is ISO 9797-1 MAC algorithm 1 under a single, double or triple length DES key, over already
// padded data.
func CBCMAC(key, padded []byte) ([]byte, error) {
	b, err := tdes(key)
	if err != nil {
		return nil, err
	}
	if len(padded)%8 != 0 || len(padded) == 0 {
		return nil, fmt.Errorf("cardcrypto: MAC data must be padded to whole blocks")
	}
	return cbcLast(b, padded), nil
}

// CMAC is AES-CMAC (NIST SP 800-38B) over data, with a 16, 24 or 32 byte key; it returns all 16 bytes.
func CMAC(key, data []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	l := make([]byte, 16)
	b.Encrypt(l, l)
	k1 := dbl(l)
	k2 := dbl(k1)
	n := (len(data) + 15) / 16
	var last []byte
	if n == 0 {
		n = 1
	}
	if len(data) > 0 && len(data)%16 == 0 {
		last = xor(data[(n-1)*16:], k1)
	} else {
		pad := PadISO2(data[(n-1)*16:], 16)
		last = xor(pad, k2)
	}
	x := make([]byte, 16)
	for i := 0; i < n-1; i++ {
		b.Encrypt(x, xor(x, data[i*16:(i+1)*16]))
	}
	b.Encrypt(x, xor(x, last))
	return x, nil
}

// dbl is multiplication by x in GF(2^128), the CMAC subkey step.
func dbl(in []byte) []byte {
	out := make([]byte, 16)
	carry := in[0] >> 7
	for i := 0; i < 15; i++ {
		out[i] = in[i]<<1 | in[i+1]>>7
	}
	out[15] = in[15] << 1
	if carry == 1 {
		out[15] ^= 0x87
	}
	return out
}

// MAC computes the MAC algorithm names over data, padding it as the algorithm requires (method 1 for
// the DES MACs, as ANSI X9.19 does; CMAC pads itself). It returns 8 bytes (DES) or 16 (CMAC).
func MAC(algorithm string, key, data []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "", MACRetail, "x9.19", "iso9797-3":
		return RetailMAC(key, PadISO1(data, 8))
	case MACCBC, "iso9797-1":
		return CBCMAC(key, PadISO1(data, 8))
	case MACCMAC, "aes-cmac":
		return CMAC(key, data)
	}
	return nil, fmt.Errorf("cardcrypto: MAC algorithm %q is not retail, cbc or cmac", algorithm)
}

// MACData is the input prova MACs on an ISO 8583 financial message: the primary account number,
// processing code, amount (with two decimals, as "90.00"), alphabetic currency, STAN and terminal id,
// each followed by a field separator (0x1C). It is a field-selection MAC, the form ANSI X9.19 allows;
// networks that MAC the whole message differ.
func MACData(pan, processingCode, amount, currency, stan, terminal string) []byte {
	var b strings.Builder
	for _, f := range []string{pan, processingCode, twoDecimals(amount), strings.ToUpper(currency), stan, terminal} {
		b.WriteString(f)
		b.WriteByte(0x1C)
	}
	return []byte(b.String())
}

// twoDecimals renders a plain amount ("90", "90.5", "90.50") with exactly two decimals; anything else
// is returned unchanged.
func twoDecimals(a string) string {
	a = strings.TrimSpace(a)
	whole, frac, dot := strings.Cut(a, ".")
	if !digitsOnly(whole) || dot && frac != "" && !digitsOnly(frac) || len(frac) > 2 || dot && frac == "" {
		return a
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	return whole + "." + (frac + "00")[:2]
}

// VerifyMAC reports whether the hex MAC was computed under key over data. A MAC shorter than the
// computed one is compared on its leading bytes (an 8-byte truncated CMAC, say).
func VerifyMAC(algorithm string, key, data []byte, macHex string) (bool, error) {
	got, err := Hex(macHex)
	if err != nil || len(got) < 4 {
		return false, fmt.Errorf("cardcrypto: MAC is not hex of at least 4 bytes")
	}
	want, err := MAC(algorithm, key, data)
	if err != nil {
		return false, err
	}
	if len(got) > len(want) {
		return false, nil
	}
	return HexUpper(got) == HexUpper(want[:len(got)]), nil
}
