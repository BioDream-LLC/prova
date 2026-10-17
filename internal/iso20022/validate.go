package iso20022

import (
	"math/big"
	"strings"
)

// The simulated host's own field checks: what a receiving bank validates before it acts.

var currencyExponent = map[string]int{
	"EUR": 2, "USD": 2, "GBP": 2, "CHF": 2, "CAD": 2, "AUD": 2, "NZD": 2, "SEK": 2, "NOK": 2, "DKK": 2,
	"PLN": 2, "CZK": 2, "HUF": 2, "SGD": 2, "HKD": 2, "CNY": 2, "INR": 2, "ZAR": 2, "MXN": 2, "BRL": 2,
	"TRY": 2, "JPY": 0, "KRW": 0, "BHD": 3, "KWD": 3, "OMR": 3, "JOD": 3, "TND": 3,
}

// knownCurrency: absent, or an ISO 4217 code the host recognises.
func knownCurrency(c string) bool {
	if c == "" {
		return true
	}
	_, ok := currencyExponent[c]
	return ok
}

// validAmount: a positive decimal with no more fraction digits than the currency allows.
func validAmount(a, ccy string) bool {
	a = strings.TrimSpace(a)
	whole, frac, _ := strings.Cut(a, ".")
	if whole == "" && frac == "" {
		return false
	}
	for _, c := range whole + frac {
		if c < '0' || c > '9' {
			return false
		}
	}
	exp, ok := currencyExponent[ccy]
	if !ok {
		exp = 2
	}
	return len(frac) <= exp && len(whole)+len(frac) <= 18 && strings.Trim(whole+frac, "0") != ""
}

// validBIC: absent, or 8 or 11 characters: bank (4 letters), country (2 letters), location and
// branch (alphanumeric).
func validBIC(b string) bool {
	if b == "" {
		return true
	}
	if len(b) != 8 && len(b) != 11 {
		return false
	}
	for i, c := range b {
		letter := c >= 'A' && c <= 'Z'
		if i < 6 && !letter || !letter && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// validIBAN applies the mod-97 check.
func validIBAN(iban string) bool {
	s := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	var b strings.Builder
	for _, c := range s[4:] + s[:4] {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			b.WriteString(big.NewInt(int64(c - 'A' + 10)).String())
		default:
			return false
		}
	}
	n, ok := new(big.Int).SetString(b.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}
