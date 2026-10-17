package qcasebook

import "strings"

// This file is the second engine's own field validation, written separately from prova's Go casebook
// (internal/verdict/validate.go) and by a different construction: character scanning and running
// remainders rather than regular expressions and arbitrary-precision arithmetic. The cross-engine test
// compares the two on every combination it generates.

// minorUnits is the ISO 4217 exponent of each recognised currency.
var minorUnits = map[string]int{
	"EUR": 2, "USD": 2, "GBP": 2, "CHF": 2, "CAD": 2, "AUD": 2, "NZD": 2, "SEK": 2, "NOK": 2, "DKK": 2,
	"PLN": 2, "CZK": 2, "HUF": 2, "SGD": 2, "HKD": 2, "CNY": 2, "INR": 2, "ZAR": 2, "MXN": 2, "BRL": 2,
	"TRY": 2, "JPY": 0, "KRW": 0, "BHD": 3, "KWD": 3, "OMR": 3, "JOD": 3, "TND": 3,
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

// invalidInstruction reports whether any present field of the request fails validation.
func invalidInstruction(r map[string]string) bool {
	ccy := strings.TrimSpace(r["Currency"])
	if ccy != "" {
		if _, ok := minorUnits[ccy]; !ok {
			return true
		}
	}
	if a := strings.TrimSpace(r["Amount"]); a != "" && !amountOK(a, ccy) {
		return true
	}
	for _, k := range []string{"DebtorBIC", "CreditBIC"} {
		if v := strings.TrimSpace(r[k]); v != "" && !bicOK(v) {
			return true
		}
	}
	for _, k := range []string{"DebtorIBAN", "CreditIBAN"} {
		if v := strings.TrimSpace(r[k]); v != "" && !ibanOK(v) {
			return true
		}
	}
	if v := strings.TrimSpace(r["PAN"]); v != "" && !luhnOK(v) {
		return true
	}
	if v, ok := r["OrderQty"]; ok && !positiveInteger(strings.TrimSpace(v)) {
		return true
	}
	if v, ok := r["Price"]; ok && !positiveDecimal(strings.TrimSpace(v), 18) {
		return true
	}
	return false
}

// amountOK: a positive plain decimal with no more fraction digits than the currency's exponent (two
// when the currency is not given) and at most 18 digits in all.
func amountOK(a, ccy string) bool {
	places := 2
	if p, ok := minorUnits[ccy]; ok {
		places = p
	}
	return positiveDecimal(a, places)
}

func positiveDecimal(a string, places int) bool {
	if strings.HasPrefix(a, "+") {
		a = a[1:]
	}
	if a == "" || a[0] == '-' {
		return false
	}
	digits, frac, dot, nonzero := 0, 0, false, false
	for i := 0; i < len(a); i++ {
		c := a[i]
		switch {
		case c == '.' && !dot:
			dot = true
		case isDigit(c):
			digits++
			if dot {
				frac++
			}
			if c != '0' {
				nonzero = true
			}
		default:
			return false
		}
	}
	return digits > 0 && digits <= 18 && frac <= places && nonzero
}

func positiveInteger(s string) bool {
	if s == "" || len(s) > 18 {
		return false
	}
	nonzero := false
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
		if s[i] != '0' {
			nonzero = true
		}
	}
	return nonzero
}

// bicOK: institution (4 letters), country (2 letters), location (2 alphanumerics), optional branch (3).
func bicOK(b string) bool {
	if len(b) != 8 && len(b) != 11 {
		return false
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if i < 6 && !isUpper(c) || i >= 6 && !isUpper(c) && !isDigit(c) {
			return false
		}
	}
	return true
}

// ibanOK computes the ISO 7064 mod 97-10 remainder digit by digit, so no large integer is needed.
func ibanOK(iban string) bool {
	s := strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if len(s) < 15 || len(s) > 34 || !isUpper(s[0]) || !isUpper(s[1]) || !isDigit(s[2]) || !isDigit(s[3]) {
		return false
	}
	rem := 0
	for _, c := range []byte(s[4:] + s[:4]) {
		switch {
		case isDigit(c):
			rem = (rem*10 + int(c-'0')) % 97
		case isUpper(c):
			v := int(c-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

// luhnOK doubles every second digit from the right, using a lookup table for the doubled digit sum.
func luhnOK(pan string) bool {
	if len(pan) < 12 || len(pan) > 19 {
		return false
	}
	doubled := [10]int{0, 2, 4, 6, 8, 1, 3, 5, 7, 9}
	sum, odd := 0, false
	for i := len(pan) - 1; i >= 0; i-- {
		if !isDigit(pan[i]) {
			return false
		}
		d := int(pan[i] - '0')
		if odd {
			d = doubled[d]
		}
		sum += d
		odd = !odd
	}
	return sum%10 == 0
}
