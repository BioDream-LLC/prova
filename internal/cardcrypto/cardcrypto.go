// Package cardcrypto is prova's card-security arithmetic: ISO 9564 PIN blocks (formats 0, 1, 3 and 4),
// ISO 9797-1 MACs (algorithms 1 and 3) and AES-CMAC, EMV application cryptograms (Visa CVN 10 and 18,
// Mastercard CVN 16) with their master- and session-key derivations, and the Visa PVV and CVV.
//
// It composes the block ciphers in Go's standard library (crypto/des, crypto/aes); it implements no
// cipher of its own. Every function is checked against independent implementations (psec, pyemv and
// pynblock) by the vectors in testdata, which oracle.py regenerates.
//
// These are test keys and test cards. prova uses this package to build the messages a tester sends and
// to prove what a host should have decided; it is not a key store and must never hold production keys.
package cardcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Hex decodes a hex string, ignoring spaces.
func Hex(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if err != nil {
		return nil, fmt.Errorf("cardcrypto: not hex: %w", err)
	}
	return b, nil
}

// HexUpper encodes bytes as upper-case hex.
func HexUpper(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

// tdes returns a Triple DES cipher for a double (16-byte) or triple (24-byte) length key. A double
// length key K1K2 is used as K1K2K1, as payment systems do.
func tdes(key []byte) (cipher.Block, error) {
	switch len(key) {
	case 16:
		k := make([]byte, 24)
		copy(k, key)
		copy(k[16:], key[:8])
		return des.NewTripleDESCipher(k)
	case 24:
		return des.NewTripleDESCipher(key)
	case 8:
		return des.NewCipher(key)
	}
	return nil, fmt.Errorf("cardcrypto: a DES key is 8, 16 or 24 bytes, not %d", len(key))
}

func ecb(b cipher.Block, data []byte, encrypt bool) ([]byte, error) {
	bs := b.BlockSize()
	if len(data)%bs != 0 {
		return nil, fmt.Errorf("cardcrypto: data is not a whole number of %d-byte blocks", bs)
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		if encrypt {
			b.Encrypt(out[i:i+bs], data[i:i+bs])
		} else {
			b.Decrypt(out[i:i+bs], data[i:i+bs])
		}
	}
	return out, nil
}

// EncryptTDES encrypts data (whole 8-byte blocks) with key in ECB mode.
func EncryptTDES(key, data []byte) ([]byte, error) {
	b, err := tdes(key)
	if err != nil {
		return nil, err
	}
	return ecb(b, data, true)
}

// DecryptTDES decrypts data (whole 8-byte blocks) with key in ECB mode.
func DecryptTDES(key, data []byte) ([]byte, error) {
	b, err := tdes(key)
	if err != nil {
		return nil, err
	}
	return ecb(b, data, false)
}

func xor(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// adjustParity sets odd parity on every byte of a DES key, as key derivations require.
func adjustParity(k []byte) []byte {
	out := append([]byte(nil), k...)
	for i, b := range out {
		ones := 0
		for v := b >> 1; v != 0; v >>= 1 {
			ones += int(v & 1)
		}
		if ones%2 == 0 {
			out[i] = b&0xFE | 1
		} else {
			out[i] = b & 0xFE
		}
	}
	return out
}

// KCV is a DES or Triple DES key check value: the first three bytes of a zero block encrypted under
// the key.
func KCV(key []byte) (string, error) {
	out, err := EncryptTDES(key, make([]byte, 8))
	if err != nil {
		return "", err
	}
	return HexUpper(out[:3]), nil
}

// KCVAES is the legacy AES key check value: the first three bytes of a zero block encrypted under the
// key.
func KCVAES(key []byte) (string, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	out := make([]byte, 16)
	b.Encrypt(out, out)
	return HexUpper(out[:3]), nil
}

var errFormat = errors.New("cardcrypto: unsupported format")
