package cardcrypto

import (
	"crypto/aes"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
)

// PIN block formats (ISO 9564-1).
const (
	Format0 = 0 // PIN field XOR PAN field; the commonest interchange format (ANSI X9.8)
	Format1 = 1 // PIN field with a transaction-unique fill; no PAN
	Format3 = 3 // as format 0, with a random A-F fill
	Format4 = 4 // AES: 16-byte PIN and PAN fields, enciphered twice (ISO 9564-1:2017)
)

// ParseFormat reads "0", "1", "3", "4" or "ISO-0" ... into a format number.
func ParseFormat(s string) (int, error) {
	s = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "ISO")
	s = strings.TrimLeft(s, "- ")
	if s == "" {
		return Format0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n != 0 && n != 1 && n != 3 && n != 4 {
		return 0, fmt.Errorf("cardcrypto: PIN block format %q is not 0, 1, 3 or 4", s)
	}
	return n, nil
}

func checkPIN(pin string) error {
	if len(pin) < 4 || len(pin) > 12 {
		return fmt.Errorf("cardcrypto: a PIN is 4 to 12 digits, not %d", len(pin))
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			return fmt.Errorf("cardcrypto: a PIN is digits only")
		}
	}
	return nil
}

func digitsOnly(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// panField is the format 0 and 3 PAN field: four zero nibbles and the 12 rightmost PAN digits
// excluding the check digit.
func panField(pan string) ([]byte, error) {
	if !digitsOnly(pan) || len(pan) < 13 {
		return nil, fmt.Errorf("cardcrypto: PAN must be at least 13 digits")
	}
	d := pan[len(pan)-13 : len(pan)-1]
	return Hex("0000" + d)
}

// randomNibbles returns n random hex nibbles in [lo, lo+span).
func randomNibbles(n int, lo, span byte) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	const digits = "0123456789ABCDEF"
	for i := range b {
		b[i] = digits[lo+b[i]%span]
	}
	return string(b)
}

// ClearPINBlock builds the clear (unenciphered) PIN block of format 0, 1 or 3.
func ClearPINBlock(format int, pin, pan string) ([]byte, error) {
	if err := checkPIN(pin); err != nil {
		return nil, err
	}
	head := fmt.Sprintf("%d%X%s", format, len(pin), pin)
	switch format {
	case Format0:
		field, _ := Hex(head + strings.Repeat("F", 16-len(head)))
		p, err := panField(pan)
		if err != nil {
			return nil, err
		}
		return xor(field, p), nil
	case Format1:
		field, _ := Hex(head + randomNibbles(16-len(head), 0, 16))
		return field, nil
	case Format3:
		field, _ := Hex(head + randomNibbles(16-len(head), 10, 6))
		p, err := panField(pan)
		if err != nil {
			return nil, err
		}
		return xor(field, p), nil
	}
	return nil, errFormat
}

// PINFromClearBlock recovers the PIN from a clear PIN block of format 0, 1 or 3, checking its
// structure (format nibble, length, digits and fill).
func PINFromClearBlock(format int, block []byte, pan string) (string, error) {
	if len(block) != 8 {
		return "", fmt.Errorf("cardcrypto: a PIN block is 8 bytes")
	}
	field := block
	if format == Format0 || format == Format3 {
		p, err := panField(pan)
		if err != nil {
			return "", err
		}
		field = xor(block, p)
	}
	h := HexUpper(field)
	if int(h[0]-'0') != format {
		return "", fmt.Errorf("cardcrypto: PIN block is not format %d", format)
	}
	n, err := strconv.ParseInt(h[1:2], 16, 8)
	if err != nil || n < 4 || n > 12 {
		return "", fmt.Errorf("cardcrypto: PIN block has an invalid PIN length")
	}
	pin, fill := h[2:2+n], h[2+n:]
	if !digitsOnly(pin) {
		return "", fmt.Errorf("cardcrypto: PIN block PIN is not digits")
	}
	switch format {
	case Format0:
		if fill != strings.Repeat("F", len(fill)) {
			return "", fmt.Errorf("cardcrypto: format 0 fill is not F")
		}
	case Format3:
		for _, c := range fill {
			if c < 'A' || c > 'F' {
				return "", fmt.Errorf("cardcrypto: format 3 fill is not A-F")
			}
		}
	}
	return pin, nil
}

// EncipherPINBlock builds and enciphers a PIN block under the PIN key: Triple DES for formats 0, 1
// and 3; AES for format 4.
func EncipherPINBlock(format int, key []byte, pin, pan string) ([]byte, error) {
	if format == Format4 {
		return encipherFormat4(key, pin, pan)
	}
	clear, err := ClearPINBlock(format, pin, pan)
	if err != nil {
		return nil, err
	}
	return EncryptTDES(key, clear)
}

// DecipherPIN deciphers a PIN block under the PIN key and recovers the PIN.
func DecipherPIN(format int, key, block []byte, pan string) (string, error) {
	if format == Format4 {
		return decipherFormat4(key, block, pan)
	}
	clear, err := DecryptTDES(key, block)
	if err != nil {
		return "", err
	}
	return PINFromClearBlock(format, clear, pan)
}

// format 4: PIN field "4" L PIN, "A" fill to 16 nibbles, then 16 random nibbles; PAN field M (PAN
// length - 12) and the PAN, zero padded to 32 nibbles.
func pinField4(pin string) []byte {
	h := fmt.Sprintf("4%X%s", len(pin), pin)
	h += strings.Repeat("A", 16-len(h)) + randomNibbles(16, 0, 16)
	b, _ := Hex(h)
	return b
}

func panField4(pan string) ([]byte, error) {
	if !digitsOnly(pan) || len(pan) > 19 {
		return nil, fmt.Errorf("cardcrypto: PAN must be 1 to 19 digits")
	}
	m := len(pan) - 12
	if m < 0 {
		m = 0
	}
	p := pan
	if len(p) < 12 {
		p = strings.Repeat("0", 12-len(p)) + p
	}
	h := strconv.Itoa(m) + p
	return Hex(h + strings.Repeat("0", 32-len(h)))
}

func encipherFormat4(key []byte, pin, pan string) ([]byte, error) {
	if err := checkPIN(pin); err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cardcrypto: format 4 needs an AES key: %w", err)
	}
	pf, err := panField4(pan)
	if err != nil {
		return nil, err
	}
	a := make([]byte, 16)
	b.Encrypt(a, pinField4(pin))
	out := make([]byte, 16)
	b.Encrypt(out, xor(a, pf))
	return out, nil
}

func decipherFormat4(key, block []byte, pan string) (string, error) {
	if len(block) != 16 {
		return "", fmt.Errorf("cardcrypto: a format 4 PIN block is 16 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cardcrypto: format 4 needs an AES key: %w", err)
	}
	pf, err := panField4(pan)
	if err != nil {
		return "", err
	}
	ib := make([]byte, 16)
	b.Decrypt(ib, block)
	field := make([]byte, 16)
	b.Decrypt(field, xor(ib, pf))
	h := HexUpper(field)
	if h[0] != '4' {
		return "", fmt.Errorf("cardcrypto: PIN block is not format 4")
	}
	n, err := strconv.ParseInt(h[1:2], 16, 8)
	if err != nil || n < 4 || n > 12 {
		return "", fmt.Errorf("cardcrypto: PIN block has an invalid PIN length")
	}
	pin, fill := h[2:2+n], h[2+n:16]
	if !digitsOnly(pin) || fill != strings.Repeat("A", len(fill)) {
		return "", fmt.Errorf("cardcrypto: format 4 PIN field is malformed")
	}
	return pin, nil
}
