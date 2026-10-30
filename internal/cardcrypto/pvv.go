package cardcrypto

import (
	"crypto/des"
	"fmt"
	"strings"
)

// decimalise takes the decimal digits of a hex string left to right, then (if more are needed) the
// letters A-F as 0-5, up to n digits: the Visa PVV and CVV method.
func decimalise(h string, n int) string {
	var b strings.Builder
	for _, c := range h {
		if c >= '0' && c <= '9' && b.Len() < n {
			b.WriteRune(c)
		}
	}
	for _, c := range h {
		if c >= 'A' && c <= 'F' && b.Len() < n {
			b.WriteByte(byte('0' + (c - 'A')))
		}
	}
	return b.String()
}

// PVV is the Visa PIN Verification Value: the transformed security parameter (the 11 rightmost PAN
// digits excluding the check digit, the PVK index and the first four PIN digits) enciphered under the
// double-length PVK pair, decimalised to four digits.
func PVV(pvk []byte, pan, pvki, pin string) (string, error) {
	if len(pan) < 12 || !digitsOnly(pan) {
		return "", fmt.Errorf("cardcrypto: PAN must be at least 12 digits")
	}
	if len(pvki) != 1 || !digitsOnly(pvki) {
		return "", fmt.Errorf("cardcrypto: PVKI is one digit")
	}
	if err := checkPIN(pin); err != nil {
		return "", err
	}
	tsp, _ := Hex(pan[len(pan)-12:len(pan)-1] + pvki + pin[:4])
	e, err := EncryptTDES(pvk, tsp)
	if err != nil {
		return "", err
	}
	return decimalise(HexUpper(e), 4), nil
}

// CVV is the Visa Card Verification Value (and Mastercard CVC) for a PAN, expiry (YYMM) and service
// code under the double-length CVK pair: the data in two 8-byte blocks, CBC under key A with the
// final block also deciphered under key B and enciphered under A, decimalised to three digits.
func CVV(cvk []byte, pan, expiry, serviceCode string) (string, error) {
	if len(cvk) != 16 {
		return "", fmt.Errorf("cardcrypto: the CVK pair is a double-length key")
	}
	d := pan + expiry + serviceCode
	if !digitsOnly(d) || len(d) > 32 {
		return "", fmt.Errorf("cardcrypto: PAN, expiry and service code must be digits, 32 at most")
	}
	d += strings.Repeat("0", 32-len(d))
	b1, _ := Hex(d[:16])
	b2, _ := Hex(d[16:])
	ka, err := des.NewCipher(cvk[:8])
	if err != nil {
		return "", err
	}
	kb, err := des.NewCipher(cvk[8:])
	if err != nil {
		return "", err
	}
	x := make([]byte, 8)
	ka.Encrypt(x, b1)
	x = xor(x, b2)
	ka.Encrypt(x, x)
	kb.Decrypt(x, x)
	ka.Encrypt(x, x)
	return decimalise(HexUpper(x), 3), nil
}
