// Package testkeys holds the published test keys and test cards prova's simulated card switch and its
// card-security scenarios share. They are test values only, printed in prova's manual so a tester can
// reproduce every PIN block, MAC and cryptogram. Never use them, or anything derived from them, for real
// cards.
package testkeys

const (
	// PINKey is the zone PIN key (double-length Triple DES) for PIN block formats 0, 1 and 3.
	PINKey = "0123456789ABCDEFFEDCBA9876543210"
	// PINKeyAES is the AES-128 PIN key for format 4.
	PINKeyAES = "00112233445566778899AABBCCDDEEFF"
	// MACKey is the double-length key for the ISO 9797-1 algorithm 3 (retail) MAC.
	MACKey = "89ABCDEF0123456776543210FEDCBA98"
	// MACKeyAES is the AES-128 key for AES-CMAC (the NIST SP 800-38B example key).
	MACKeyAES = "2B7E151628AED2A6ABF7158809CF4F3C"
	// IMK is the issuer master key for application cryptograms.
	IMK = "11223344556677888877665544332211"
)

// Card is a test card: its number, PIN and the cryptogram version its chip computes.
type Card struct {
	PAN, PIN, Cryptogram string
}

// Cards are the chip cards the switch knows the PIN and cryptogram version of.
var Cards = []Card{
	{PAN: "4111111111111111", PIN: "1234", Cryptogram: "visa-cvn10"},
	{PAN: "4012888888881881", PIN: "4321", Cryptogram: "visa-cvn18"},
	{PAN: "5105105105105100", PIN: "2580", Cryptogram: "mc-cvn16"},
}

// CardFor returns the test card with this number.
func CardFor(pan string) (Card, bool) {
	for _, c := range Cards {
		if c.PAN == pan {
			return c, true
		}
	}
	return Card{}, false
}
