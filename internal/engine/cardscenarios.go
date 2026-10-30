package engine

import (
	"fmt"

	"github.com/biodream-llc/prova/internal/cardcrypto"
	"github.com/biodream-llc/prova/internal/testkeys"
)

// cardSecurityScenarios are the ISO 8583 scenarios whose PIN blocks, MACs and chip cryptograms are
// real: computed here from the published test keys (internal/testkeys), so the switch can verify them
// and prova can prove what it should have decided. Each seeds the keys in its context, the way a tester
// who holds the test keys knows them.
func cardSecurityScenarios() []Scenario {
	k := func(h string) []byte { b, _ := cardcrypto.Hex(h); return b }
	pinBlock := func(format int, key, pin, pan string) string {
		b, err := cardcrypto.EncipherPINBlock(format, k(key), pin, pan)
		if err != nil {
			panic("engine: test PIN block: " + err.Error())
		}
		return cardcrypto.HexUpper(b)
	}
	mac := func(alg, key, pan, amount, ccy, stan string) string {
		m, err := cardcrypto.MAC(alg, k(key), cardcrypto.MACData(pan, "000000", amount, ccy, stan, "TERM0001"))
		if err != nil {
			panic("engine: test MAC: " + err.Error())
		}
		return cardcrypto.HexUpper(m[:8])
	}
	chip := func(version, imk, pan string, amount int64, atc string) string {
		icc, err := cardcrypto.BuildChipData(cardcrypto.ChipData{Version: version, IMK: k(imk), PAN: pan, PSN: "01",
			Amount: amount, Currency: "978", Country: "276", Date: "261015", UN: "1A2B3C4D", ATC: atc})
		if err != nil {
			panic("engine: test chip data: " + err.Error())
		}
		return icc
	}
	const counterfeitIMK = "0123456789ABCDEFFEDCBA9876543210" // not the issuer's: a counterfeit chip
	type card struct{ pan, pin, version string }
	visa10 := card{"4111111111111111", "1234", cardcrypto.VisaCVN10}
	visa18 := card{"4012888888881881", "4321", cardcrypto.VisaCVN18}
	mc16 := card{"5105105105105100", "2580", cardcrypto.MCCVN16}
	for _, c := range []card{visa10, visa18, mc16} {
		if tc, ok := testkeys.CardFor(c.pan); !ok || tc.PIN != c.pin || tc.Cryptogram != c.version {
			panic("engine: card-security scenarios disagree with internal/testkeys on " + c.pan)
		}
	}
	keys := func(c card) map[string]string {
		return kv("availableBalance", "142.00", "pinKey", testkeys.PINKey, "cardPIN", c.pin,
			"macKey", testkeys.MACKey, "imk", testkeys.IMK, "cryptogramVersion", c.version)
	}
	// purchase is a 90.00 chip purchase on card c with the given STAN, carrying a correct PIN block,
	// MAC and cryptogram unless the caller replaces one.
	purchase := func(c card, stan string) map[string]string {
		return map[string]string{
			"PAN": c.pan, "ProcessingCode": "000000", "Amount": "90.00", "Currency": "EUR", "STAN": stan,
			"Terminal": "TERM0001", "Expiry": "3012", "POSEntryMode": "051",
			"PINBlock": pinBlock(cardcrypto.Format0, testkeys.PINKey, c.pin, c.pan),
			"MAC":      mac(cardcrypto.MACRetail, testkeys.MACKey, c.pan, "90.00", "EUR", stan),
			"ICCData":  chip(c.version, testkeys.IMK, c.pan, 9000, "0042"),
		}
	}
	p := "iso8583"
	sc := func(id, name, desc string, req, ctx map[string]string, expect string) Scenario {
		return Scenario{ID: id, Name: name, Protocol: p, Description: desc, Request: req, Context: ctx, Expect: expect}
	}
	out := []Scenario{
		sc("iso8583-chip-valid", "ISO 8583 0100 — chip purchase, PIN, MAC and ARQC all correct",
			"A chip-read (DE22=051) authorisation of 90.00 on a Visa card (CVN 10) whose PIN block, MAC and Application Cryptogram (9F26) are computed from the published test keys. Within balance, the switch must approve.",
			purchase(visa10, "000120"), keys(visa10), "approve (DE39=00)"),
		sc("iso8583-pin-wrong", "ISO 8583 0100 — wrong PIN",
			"The cardholder enters 9999 on a card whose PIN is 1234. The PIN block deciphers to the wrong PIN under the zone PIN key: the switch must decline with DE39=55 (incorrect PIN).",
			with(purchase(visa10, "000125"), "PINBlock", pinBlock(cardcrypto.Format0, testkeys.PINKey, "9999", visa10.pan)),
			with(keys(visa10), "expectReason", "55"), "decline (DE39=55)"),
		sc("iso8583-pin-format4", "ISO 8583 0100 — AES PIN block (ISO format 4)",
			"The PIN travels in an ISO 9564 format 4 block enciphered under an AES PIN key. It is the card's PIN: the switch must approve.",
			with(purchase(visa10, "000126"), "PINBlock", pinBlock(cardcrypto.Format4, testkeys.PINKeyAES, visa10.pin, visa10.pan)),
			with(keys(visa10), "pinKey", testkeys.PINKeyAES, "pinFormat", "4"), "approve (DE39=00)"),
		sc("iso8583-pin-format4-wrong", "ISO 8583 0100 — wrong PIN in an AES PIN block",
			"A format 4 (AES) PIN block carrying 0000 for a card whose PIN is 1234. The switch must decline with DE39=55.",
			with(purchase(visa10, "000127"), "PINBlock", pinBlock(cardcrypto.Format4, testkeys.PINKeyAES, "0000", visa10.pan)),
			with(keys(visa10), "pinKey", testkeys.PINKeyAES, "pinFormat", "4", "expectReason", "55"), "decline (DE39=55)"),
		sc("iso8583-mac-wrong", "ISO 8583 0100 — MAC does not match the message",
			"The MAC was computed over 91.00 but the message says 90.00: the message was altered after it was MACed. The switch must decline with DE39=63 (security violation).",
			with(purchase(visa10, "000128"), "MAC", mac(cardcrypto.MACRetail, testkeys.MACKey, visa10.pan, "91.00", "EUR", "000128")),
			with(keys(visa10), "expectReason", "63"), "decline (DE39=63)"),
		sc("iso8583-mac-cmac", "ISO 8583 0100 — AES-CMAC message authentication",
			"The MAC is an AES-CMAC (NIST SP 800-38B), truncated to 8 bytes, under the AES MAC key. It is correct: the switch must approve.",
			with(purchase(visa10, "000129"), "MAC", mac(cardcrypto.MACCMAC, testkeys.MACKeyAES, visa10.pan, "90.00", "EUR", "000129")),
			with(keys(visa10), "macKey", testkeys.MACKeyAES, "macAlgorithm", "cmac"), "approve (DE39=00)"),
		sc("iso8583-arqc-wrong", "ISO 8583 0100 — counterfeit chip (ARQC fails)",
			"The chip data is well formed, but its cryptogram was computed under a key that is not the issuer's, as a counterfeit chip's would be. The switch must decline with DE39=63.",
			with(purchase(visa10, "000130"), "ICCData", chip(visa10.version, counterfeitIMK, visa10.pan, 9000, "0042")),
			with(keys(visa10), "expectReason", "63"), "decline (DE39=63)"),
		sc("iso8583-arqc-amount-altered", "ISO 8583 0100 — chip data altered after the card signed it",
			"The cryptogram covers an amount of 90.00, but the chip data's amount (9F02) was changed to 900.00 afterwards. The cryptogram no longer verifies: the switch must decline with DE39=63.",
			with(purchase(visa10, "000131"), "ICCData", alterTag(chip(visa10.version, testkeys.IMK, visa10.pan, 9000, "0042"), "9F02", "000000090000")),
			with(keys(visa10), "expectReason", "63"), "decline (DE39=63)"),
		sc("iso8583-arqc-cvn18", "ISO 8583 0100 — Visa CVN 18 cryptogram",
			"A chip purchase on a Visa card whose chip computes CVN 18 cryptograms (option B master key, EMV common session key). PIN, MAC and ARQC are correct: the switch must approve.",
			purchase(visa18, "000132"), keys(visa18), "approve (DE39=00)"),
		sc("iso8583-arqc-cvn16", "ISO 8583 0100 — Mastercard CVN 16 cryptogram",
			"A chip purchase on a Mastercard card whose chip computes CVN 16 cryptograms (Mastercard session key from the ATC and unpredictable number). All correct: the switch must approve.",
			purchase(mc16, "000133"), keys(mc16), "approve (DE39=00)"),
	}
	// The same checks over the Visa-style and Mastercard-style wire dialects, where a cryptogram failure
	// has the scheme's own code.
	for _, d := range []struct {
		target, label, code string
		c                   card
	}{{"visa-switch", "Visa dialect", "82", visa10}, {"mc-switch", "Mastercard dialect", "88", mc16}} {
		id := map[string]string{"visa-switch": "visa", "mc-switch": "mc"}[d.target]
		valid := sc(id+"-chip-valid", "ISO 8583 0100 ("+d.label+") — chip purchase, all correct",
			fmt.Sprintf("A chip purchase in the %s: BCD or EBCDIC numbers, binary bitmaps, binary PIN block, MAC and chip data. All correct: the switch must approve.", d.label),
			purchase(d.c, "000140"), keys(d.c), "approve (DE39=00)")
		valid.Target = d.target
		wrong := sc(id+"-arqc-wrong", "ISO 8583 0100 ("+d.label+") — counterfeit chip",
			fmt.Sprintf("A counterfeit chip's cryptogram in the %s. The switch must decline with the scheme's cryptogram-failure code, DE39=%s.", d.label, d.code),
			with(purchase(d.c, "000141"), "ICCData", chip(d.c.version, counterfeitIMK, d.c.pan, 9000, "0042")),
			with(keys(d.c), "expectReason", d.code), "decline (DE39="+d.code+")")
		wrong.Target = d.target
		pin := sc(id+"-pin-wrong", "ISO 8583 0100 ("+d.label+") — wrong PIN",
			fmt.Sprintf("A wrong PIN in the %s. The switch must decline with DE39=55.", d.label),
			with(purchase(d.c, "000142"), "PINBlock", pinBlock(cardcrypto.Format0, testkeys.PINKey, "0000", d.c.pan)),
			with(keys(d.c), "expectReason", "55"), "decline (DE39=55)")
		pin.Target = d.target
		out = append(out, valid, wrong, pin)
	}
	return out
}

// alterTag replaces one tag's value in hex chip data.
func alterTag(icc, tag, value string) string {
	objs, err := cardcrypto.ParseTLV(icc)
	if err != nil {
		return icc
	}
	v, _ := cardcrypto.Hex(value)
	for i := range objs {
		if objs[i].Tag == tag {
			objs[i].Value = v
		}
	}
	return cardcrypto.BuildTLV(objs)
}
