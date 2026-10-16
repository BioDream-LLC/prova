// Package synth generates realistic, standards-valid synthetic financial data for scenarios: card
// numbers that pass the Luhn check, IBANs with a correct mod-97 checksum, well-formed BICs, amounts
// and identifiers. It is seedable, so a data-driven run is reproducible.
package synth

import (
	"fmt"
	"math/rand"
	"strings"
)

// Gen is a seeded generator. Use New for a reproducible stream.
type Gen struct{ r *rand.Rand }

// New returns a generator seeded deterministically.
func New(seed int64) *Gen { return &Gen{r: rand.New(rand.NewSource(seed))} }

const digits = "0123456789"
const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func (g *Gen) pick(set string) byte { return set[g.r.Intn(len(set))] }

func (g *Gen) run(set string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = g.pick(set)
	}
	return string(b)
}

// PAN returns a Luhn-valid card number with the given IIN prefix, padded to 16 digits.
func (g *Gen) PAN(prefix string) string {
	if prefix == "" {
		prefix = "4"
	}
	body := prefix
	for len(body) < 15 {
		body += string(digits[g.r.Intn(10)])
	}
	body = body[:15]
	return body + string(digits[luhnCheck(body)])
}

// luhnCheck returns the Luhn check digit for a numeric string.
func luhnCheck(num string) int {
	sum, alt := 0, true
	for i := len(num) - 1; i >= 0; i-- {
		d := int(num[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return (10 - sum%10) % 10
}

// LuhnValid reports whether a numeric string satisfies the Luhn checksum.
func LuhnValid(num string) bool {
	sum, alt := 0, false
	for i := len(num) - 1; i >= 0; i-- {
		d := int(num[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// Amount returns a decimal amount string (2 dp) uniformly in [min, max].
func (g *Gen) Amount(min, max float64) string {
	if max < min {
		min, max = max, min
	}
	v := min + g.r.Float64()*(max-min)
	return fmt.Sprintf("%.2f", v)
}

// BIC returns a well-formed 11-character BIC: 4-letter institution, 2-letter country, 2 location,
// 3 branch.
func (g *Gen) BIC(country string) string {
	if len(country) != 2 {
		country = g.run(upper, 2)
	}
	return g.run(upper, 4) + strings.ToUpper(country) + g.run(upper+digits, 2) + g.run(upper+digits, 3)
}

// IBAN returns an IBAN with a valid mod-97 check for the given 2-letter country (default DE),
// using an 18-digit BBAN.
func (g *Gen) IBAN(country string) string {
	if len(country) != 2 {
		country = "DE"
	}
	country = strings.ToUpper(country)
	bban := g.run(digits, 18)
	check := ibanCheck(country, bban)
	return fmt.Sprintf("%s%02d%s", country, check, bban)
}

// ibanCheck computes the two-digit IBAN check number (so the whole IBAN mod 97 == 1).
func ibanCheck(country, bban string) int {
	// rearrange: BBAN + country + "00", letters -> numbers (A=10..Z=35), then 98 - (n mod 97)
	rearranged := bban + country + "00"
	var sb strings.Builder
	for _, c := range rearranged {
		switch {
		case c >= '0' && c <= '9':
			sb.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			sb.WriteString(fmt.Sprintf("%d", c-'A'+10))
		}
	}
	return 98 - mod97(sb.String())
}

// IBANValid reports whether an IBAN has a correct mod-97 checksum.
func IBANValid(iban string) bool {
	iban = strings.ToUpper(strings.ReplaceAll(iban, " ", ""))
	if len(iban) < 5 {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	var sb strings.Builder
	for _, c := range rearranged {
		switch {
		case c >= '0' && c <= '9':
			sb.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			sb.WriteString(fmt.Sprintf("%d", c-'A'+10))
		default:
			return false
		}
	}
	return mod97(sb.String()) == 1
}

// mod97 computes a big decimal string mod 97 without bignum.
func mod97(s string) int {
	rem := 0
	for _, c := range s {
		rem = (rem*10 + int(c-'0')) % 97
	}
	return rem
}

// E2E returns a unique end-to-end identifier.
func (g *Gen) E2E() string { return "E2E-" + g.run(upper+digits, 10) }

// Account returns a synthetic account identifier.
func (g *Gen) Account() string { return "ACC-" + g.run(upper+digits, 8) }

// STAN returns a 6-digit System Trace Audit Number (ISO 8583 DE11).
func (g *Gen) STAN() string { return g.run(digits, 6) }
