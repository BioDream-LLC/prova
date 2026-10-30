package engine

import (
	"fmt"
	"strings"

	"github.com/biodream-llc/prova/internal/cardcrypto"
	"github.com/biodream-llc/prova/internal/testkeys"
)

// Generated scenario families: the same control across every value that matters (twenty countries'
// IBANs, each currency exponent, each card-number length, each PIN block format and cryptogram version
// on each wire dialect, each FIX session version). Each is an ordinary scenario with a stated expected
// outcome; the test suite runs every one against the compliant and the defective targets.

// ibanExamples are valid IBANs from the national examples of the SWIFT IBAN registry.
var ibanExamples = []string{
	"AT611904300234573201", "BE68539007547034", "CH9300762011623852957", "CZ6508000000192000145399", "DE89370400440532013000",
	"DK5000400440116243", "ES9121000418450200051332", "FI2112345600000785", "FR1420041010050500013M02606", "GB29NWBK60161331926819",
	"IE29AIBK93115212345678", "IT60X0542811101000000123456", "LU280019400644750000", "NL91ABNA0417164300", "NO9386011117947",
	"PL61109010140000071219812874", "PT50000201231234567890154", "SE4550000000058398257466", "SI56263300012039086", "SK3112000000198742637541",
}

func generatedScenarios() []Scenario {
	var out []Scenario
	out = append(out, ibanFamily()...)
	out = append(out, boundaryFamily()...)
	out = append(out, exponentFamily()...)
	out = append(out, bicFamily()...)
	out = append(out, panFamily()...)
	out = append(out, dialectFamily()...)
	out = append(out, cryptoFamily()...)
	out = append(out, fixVersionFamily()...)
	out = append(out, fixFieldFamily()...)
	return out
}

func mxPay(e2e, amount, acct string) map[string]string {
	return map[string]string{"MsgId": "MSG-" + e2e, "EndToEndId": e2e, "Amount": amount, "Currency": "EUR",
		"DebtorAcct": acct, "CreditAcct": "ACC-FR-9", "DebtorBIC": "BANKDEFFXXX", "CreditBIC": "BANKFRPPXXX"}
}

// ibanFamily: a payment to each country's IBAN is accepted; with its check digits broken it is refused
// with AC01.
func ibanFamily() []Scenario {
	var out []Scenario
	bal := kv("availableBalance", "142.00")
	for _, iban := range ibanExamples {
		cc := iban[:2]
		bad := cc + "00" + iban[4:] // check digits 00 never pass mod 97
		out = append(out,
			Scenario{ID: "iban-" + strings.ToLower(cc) + "-valid", Name: "IBAN " + cc + " — a valid creditor IBAN", Protocol: "iso20022",
				Description: fmt.Sprintf("Pay 90.00 to %s, the %s example IBAN, whose check digits are right. The host must accept.", iban, cc),
				Request:     with(mxPay("E2E-IBAN-"+cc, "90.00", "ACC-EUR-1"), "CreditIBAN", iban), Context: bal, Expect: "accept (ACSP)"},
			Scenario{ID: "iban-" + strings.ToLower(cc) + "-check-digits", Name: "IBAN " + cc + " — creditor IBAN with wrong check digits", Protocol: "iso20022",
				Description: fmt.Sprintf("Pay 90.00 to %s: the %s IBAN with its check digits set to 00, which never passes the ISO 13616 mod-97 check. The host must refuse with AC01.", bad, cc),
				Request:     with(mxPay("E2E-IBANX-"+cc, "90.00", "ACC-EUR-1"), "CreditIBAN", bad), Context: with(bal, "expectReason", "AC01"), Expect: "reject (AC01)"})
	}
	return out
}

// boundaryFamily: one cent either side of the balance, for every message that debits an account.
func boundaryFamily() []Scenario {
	var out []Scenario
	bal := kv("availableBalance", "142.00")
	for _, m := range []struct{ kind, label string }{
		{"pacs.008", "pacs.008"}, {"pacs.009", "pacs.009"}, {"pain.001", "pain.001"}, {"pacs.003", "pacs.003"}, {"pacs.010", "pacs.010"},
	} {
		id := strings.ReplaceAll(m.kind, ".", "")
		under := mxPay("E2E-B-U-"+id, "141.99", "ACC-EUR-1")
		over := mxPay("E2E-B-O-"+id, "142.01", "ACC-EUR-1")
		if m.kind == "pacs.003" {
			under["MandateId"], over["MandateId"] = "MNDT-1", "MNDT-1"
		}
		out = append(out,
			Scenario{ID: "boundary-" + id + "-cent-under", Name: m.label + " — one cent under the balance", Protocol: "iso20022", Message: m.kind,
				Description: "Debit 141.99 from an account holding 142.00. The host must accept.", Request: under, Context: bal, Expect: "accept"},
			Scenario{ID: "boundary-" + id + "-cent-over", Name: m.label + " — one cent over the balance", Protocol: "iso20022", Message: m.kind,
				Description: "Debit 142.01 from an account holding 142.00. One cent short: the host must refuse with AM04.", Request: over,
				Context: with(bal, "expectReason", "AM04"), Expect: "reject (AM04)"})
	}
	return out
}

// exponentFamily: each currency's minor units (ISO 4217 exponent 0, 2 or 3), right and one decimal too many.
func exponentFamily() []Scenario {
	var out []Scenario
	for _, c := range []struct{ ccy, acct, ok, bad, bal string }{
		{"JPY", "ACC-JPY-1", "5000", "5000.5", "1000000.00"}, {"USD", "ACC-USD-1", "12.34", "12.345", "1000.00"},
		{"GBP", "ACC-GBP-1", "12.34", "12.345", "1000.00"}, {"CHF", "ACC-CHF-1", "12.34", "12.345", "1000.00"},
		{"BHD", "ACC-BHD-1", "12.345", "12.3456", "1000.00"}, {"KWD", "ACC-KWD-1", "1.250", "1.2505", "1000.00"},
	} {
		ctx := kv("availableBalance", c.bal, "accountCurrency", c.ccy)
		id := strings.ToLower(c.ccy)
		out = append(out,
			Scenario{ID: "currency-" + id + "-exponent", Name: "pacs.008 — " + c.ccy + " amount with its minor units", Protocol: "iso20022",
				Description: fmt.Sprintf("Pay %s %s from a %s account. The amount has no more decimals than %s allows: the host must accept.", c.ok, c.ccy, c.ccy, c.ccy),
				Request:     with(mxPay("E2E-CCY-"+c.ccy, c.ok, c.acct), "Currency", c.ccy), Context: ctx, Expect: "accept"},
			Scenario{ID: "currency-" + id + "-too-many-decimals", Name: "pacs.008 — " + c.ccy + " amount with a decimal too many", Protocol: "iso20022",
				Description: fmt.Sprintf("Pay %s %s: one decimal more than %s's minor units allow. The host must refuse with AM12 (invalid amount).", c.bad, c.ccy, c.ccy),
				Request:     with(mxPay("E2E-CCYX-"+c.ccy, c.bad, c.acct), "Currency", c.ccy), Context: with(ctx, "expectReason", "AM12"), Expect: "reject (AM12)"})
	}
	return out
}

// bicFamily: well-formed and malformed creditor agent BICs.
func bicFamily() []Scenario {
	var out []Scenario
	bal := kv("availableBalance", "142.00")
	for _, b := range []struct{ id, bic, why string }{
		{"bic-8", "BANKFRPP", ""}, {"bic-11", "BANKFRPPXXX", ""}, {"bic-lowercase", "bankfrpp", "is in lower case"},
		{"bic-7", "BANKFRP", "has 7 characters"}, {"bic-10", "BANKFRPPXX", "has 10 characters"}, {"bic-digit-country", "BANK1RPP", "has a digit in its country code"},
	} {
		sc := Scenario{ID: b.id, Protocol: "iso20022", Request: with(mxPay("E2E-"+strings.ToUpper(b.id), "90.00", "ACC-EUR-1"), "CreditBIC", b.bic), Context: bal}
		if b.why == "" {
			sc.Name, sc.Description, sc.Expect = "pacs.008 — creditor agent BIC "+b.bic, "Address the creditor agent as "+b.bic+", a well-formed BIC. The host must accept.", "accept"
		} else {
			sc.Name = "pacs.008 — malformed creditor agent BIC " + b.bic
			sc.Description = "Address the creditor agent as " + b.bic + ", which " + b.why + ". The host must refuse with RC01."
			sc.Context, sc.Expect = with(bal, "expectReason", "RC01"), "reject (RC01)"
		}
		out = append(out, sc)
	}
	return out
}

// panFamily: card numbers of every length the Luhn check covers, and card expiry either side of the
// switch's business date.
func panFamily() []Scenario {
	var out []Scenario
	bal := kv("availableBalance", "142.00")
	card := func(pan, stan string) map[string]string {
		return map[string]string{"PAN": pan, "ProcessingCode": "000000", "Amount": "90.00", "Currency": "EUR", "STAN": stan, "Terminal": "TERM0001", "Expiry": "3012"}
	}
	for i, pan := range []string{"4222222222222", "378282246310005", "6011000990139424", "6011000000000000001"} {
		last := pan[len(pan)-1] - '0'
		bad := pan[:len(pan)-1] + string(rune('0'+(last+1)%10))
		n := len(pan)
		out = append(out,
			Scenario{ID: fmt.Sprintf("pan-%d-digits", n), Name: fmt.Sprintf("ISO 8583 0100 — a %d-digit card number", n), Protocol: "iso8583",
				Description: fmt.Sprintf("Authorise 90.00 on %s, a %d-digit card number that passes the Luhn check. Within balance, the switch must approve.", pan, n),
				Request:     card(pan, fmt.Sprintf("0003%02d", i)), Context: bal, Expect: "approve (DE39=00)"},
			Scenario{ID: fmt.Sprintf("pan-%d-digits-luhn", n), Name: fmt.Sprintf("ISO 8583 0100 — a %d-digit card number failing the Luhn check", n), Protocol: "iso8583",
				Description: fmt.Sprintf("Authorise 90.00 on %s, whose last digit is wrong. The switch must decline with DE39=14.", bad),
				Request:     card(bad, fmt.Sprintf("0003%02d", 10+i)), Context: with(bal, "expectReason", "14"), Expect: "decline (DE39=14)"})
	}
	for _, e := range []struct {
		exp, label string
		ok         bool
	}{{"2610", "expiring this month", true}, {"2611", "expiring next month", true}, {"2609", "expired last month", false}, {"2601", "expired in January", false}} {
		sc := Scenario{ID: "expiry-" + e.exp, Name: "ISO 8583 0100 — a card " + e.label, Protocol: "iso8583",
			Request: with(card("4111111111111111", "0004"+e.exp[2:]), "Expiry", e.exp)}
		ctx := with(bal, "processingDate", "2026-10-15")
		if e.ok {
			sc.Description = "Authorise 90.00 on a card with expiry " + e.exp + " on 15 October 2026. A card is good through the end of its expiry month: the switch must approve."
			sc.Context, sc.Expect = ctx, "approve (DE39=00)"
		} else {
			sc.Description = "Authorise 90.00 on a card with expiry " + e.exp + " on 15 October 2026. The card has expired: the switch must decline with DE39=54."
			sc.Context, sc.Expect = with(ctx, "expectReason", "54"), "decline (DE39=54)"
		}
		out = append(out, sc)
	}
	return out
}

var dialects = []struct{ id, target, label, arqcCode string }{
	{"generic", "", "generic ASCII", "63"}, {"visa", "visa-switch", "Visa dialect", "82"}, {"mc", "mc-switch", "Mastercard dialect", "88"},
}

// dialectFamily: the balance boundary for authorisations and purchases on each wire dialect.
func dialectFamily() []Scenario {
	var out []Scenario
	bal := kv("availableBalance", "142.00")
	n := 0
	for _, d := range dialects {
		for _, mti := range []string{"0100", "0200"} {
			for _, a := range []struct {
				amt, label string
				ok         bool
			}{{"141.99", "a cent under", true}, {"142.00", "exactly", true}, {"142.01", "a cent over", false}} {
				sc := Scenario{ID: fmt.Sprintf("%s-%s-%s", d.id, mti, strings.ReplaceAll(a.amt, ".", "")), Protocol: "iso8583", Message: mti, Target: d.target,
					Name: fmt.Sprintf("ISO 8583 %s (%s) — %s the balance", mti, d.label, a.label),
					Request: map[string]string{"PAN": "4111111111111111", "ProcessingCode": "000000", "Amount": a.amt, "Currency": "EUR",
						"STAN": fmt.Sprintf("0005%02d", n), "Terminal": "TERM0001", "Expiry": "3012"}}
				n++
				if a.ok {
					sc.Description = fmt.Sprintf("%s for %s on a card holding 142.00, in the %s. The switch must approve.", mti, a.amt, d.label)
					sc.Context, sc.Expect = bal, "approve (DE39=00)"
				} else {
					sc.Description = fmt.Sprintf("%s for %s on a card holding 142.00, in the %s. The switch must decline with DE39=51.", mti, a.amt, d.label)
					sc.Context, sc.Expect = with(bal, "expectReason", "51"), "decline (DE39=51)"
				}
				out = append(out, sc)
			}
		}
	}
	return out
}

// cryptoFamily: every PIN block format, MAC algorithm and cryptogram version, right and wrong, on each
// dialect. Format 4 PIN blocks are 16 bytes and travel only in the generic dialect, whose DE52 is
// variable length.
func cryptoFamily() []Scenario {
	k := func(h string) []byte { b, _ := cardcrypto.Hex(h); return b }
	type card struct{ pan, pin, version string }
	cards := []card{{"4111111111111111", "1234", cardcrypto.VisaCVN10}, {"4012888888881881", "4321", cardcrypto.VisaCVN18}, {"5105105105105100", "2580", cardcrypto.MCCVN16}}
	base := func(c card, stan string) map[string]string {
		chip, _ := cardcrypto.BuildChipData(cardcrypto.ChipData{Version: c.version, IMK: k(testkeys.IMK), PAN: c.pan, PSN: "01",
			Amount: 9000, Currency: "978", Country: "276", Date: "261015", UN: "1A2B3C4D", ATC: "0042"})
		return map[string]string{"PAN": c.pan, "ProcessingCode": "000000", "Amount": "90.00", "Currency": "EUR", "STAN": stan,
			"Terminal": "TERM0001", "Expiry": "3012", "POSEntryMode": "051", "ICCData": chip}
	}
	keys := func(c card) map[string]string {
		return kv("availableBalance", "142.00", "pinKey", testkeys.PINKey, "cardPIN", c.pin, "macKey", testkeys.MACKey,
			"imk", testkeys.IMK, "cryptogramVersion", c.version)
	}
	var out []Scenario
	n := 0
	add := func(d struct{ id, target, label, arqcCode string }, id, name, desc string, req, ctx map[string]string, expect string) {
		out = append(out, Scenario{ID: d.id + "-" + id, Name: "ISO 8583 0100 (" + d.label + ") — " + name, Protocol: "iso8583",
			Description: desc + " (" + d.label + ")", Request: req, Context: ctx, Expect: expect, Target: d.target})
	}
	stan := func() string { n++; return fmt.Sprintf("0006%02d", n%100) }
	for _, d := range dialects {
		v := cards[0]
		formats := []int{cardcrypto.Format0, cardcrypto.Format1, cardcrypto.Format3}
		if d.id == "generic" {
			formats = append(formats, cardcrypto.Format4)
		}
		for _, f := range formats {
			key, ctxKey := testkeys.PINKey, kv()
			if f == cardcrypto.Format4 {
				key, ctxKey = testkeys.PINKeyAES, kv("pinKey", testkeys.PINKeyAES)
			}
			ctxKey["pinFormat"] = fmt.Sprint(f)
			right, _ := cardcrypto.EncipherPINBlock(f, k(key), v.pin, v.pan)
			wrong, _ := cardcrypto.EncipherPINBlock(f, k(key), "0000", v.pan)
			ctx := keys(v)
			for kk, vv := range ctxKey {
				ctx[kk] = vv
			}
			add(d, fmt.Sprintf("pin-format%d", f), fmt.Sprintf("PIN block format %d, right PIN", f),
				fmt.Sprintf("The PIN travels in an ISO 9564 format %d block and is the card's. The switch must approve.", f),
				with(base(v, stan()), "PINBlock", cardcrypto.HexUpper(right)), ctx, "approve (DE39=00)")
			add(d, fmt.Sprintf("pin-format%d-wrong", f), fmt.Sprintf("PIN block format %d, wrong PIN", f),
				fmt.Sprintf("A format %d PIN block carrying 0000 for a card whose PIN is %s. The switch must decline with DE39=55.", f, v.pin),
				with(base(v, stan()), "PINBlock", cardcrypto.HexUpper(wrong)), with(ctx, "expectReason", "55"), "decline (DE39=55)")
		}
		for _, m := range []struct{ alg, key, label string }{{cardcrypto.MACRetail, testkeys.MACKey, "retail MAC (ISO 9797-1 algorithm 3)"}, {cardcrypto.MACCMAC, testkeys.MACKeyAES, "AES-CMAC"}} {
			s1, s2 := stan(), stan()
			mk := func(amount, s string) string {
				mac, _ := cardcrypto.MAC(m.alg, k(m.key), cardcrypto.MACData(v.pan, "000000", amount, "EUR", s, "TERM0001"))
				return cardcrypto.HexUpper(mac[:8])
			}
			ctx := with(keys(v), "macKey", m.key, "macAlgorithm", m.alg)
			add(d, "mac-"+m.alg, m.label+", right", "A "+m.label+" over prova's MAC fields under the MAC key. The switch must approve.",
				with(base(v, s1), "MAC", mk("90.00", s1)), ctx, "approve (DE39=00)")
			add(d, "mac-"+m.alg+"-altered", m.label+", message altered", "A "+m.label+" computed over 91.00 on a message for 90.00. The switch must decline with DE39=63.",
				with(base(v, s2), "MAC", mk("91.00", s2)), with(ctx, "expectReason", "63"), "decline (DE39=63)")
		}
		for _, c := range cards {
			ver := strings.ToUpper(strings.ReplaceAll(c.version, "-", " "))
			good := base(c, stan())
			add(d, "arqc-"+c.version, ver+" cryptogram, right", "A chip purchase whose "+ver+" ARQC is computed from the issuer master key. The switch must approve.",
				good, keys(c), "approve (DE39=00)")
			fake, _ := cardcrypto.BuildChipData(cardcrypto.ChipData{Version: c.version, IMK: k("0123456789ABCDEFFEDCBA9876543210"), PAN: c.pan, PSN: "01",
				Amount: 9000, Currency: "978", Country: "276", Date: "261015", UN: "1A2B3C4D", ATC: "0042"})
			add(d, "arqc-"+c.version+"-counterfeit", ver+" cryptogram, counterfeit chip", "A "+ver+" ARQC computed under a key that is not the issuer's. The switch must decline with DE39="+d.arqcCode+".",
				with(base(c, stan()), "ICCData", fake), with(keys(c), "expectReason", d.arqcCode), "decline (DE39="+d.arqcCode+")")
			add(d, "arqc-"+c.version+"-atc-altered", ver+" cryptogram, counter altered", "The chip's transaction counter (9F36) was changed after the card computed its "+ver+" ARQC. The switch must decline with DE39="+d.arqcCode+".",
				with(base(c, stan()), "ICCData", alterTag(good["ICCData"], "9F36", "0043")), with(keys(c), "expectReason", d.arqcCode), "decline (DE39="+d.arqcCode+")")
		}
	}
	return out
}

// fixVersionFamily: the order controls, the order lifecycle and the session rules on FIX 4.2 and FIX
// 5.0 SP2 sessions.
func fixVersionFamily() []Scenario {
	var base []Scenario
	for _, sc := range fixScenarios() {
		if strings.HasPrefix(sc.ID, "fix-") && !strings.HasPrefix(sc.ID, "fix-mass-cancel") && sc.Target == "" {
			base = append(base, sc)
		}
	}
	written := map[string]bool{}
	for _, sc := range fixScenarios() {
		written[sc.ID] = true
	}
	var out []Scenario
	for _, v := range []struct{ id, target, label string }{{"fix42", "fix42-venue", "FIX 4.2"}, {"fix50", "fix50-venue", "FIX 5.0 SP2"}} {
		for _, sc := range base {
			c := sc
			c.ID = v.id + strings.TrimPrefix(sc.ID, "fix")
			if written[c.ID] {
				continue // written by hand in fixScenarios
			}
			c.Target = v.target
			c.Name = strings.Replace(sc.Name, "FIX ", v.label+" ", 1)
			c.Description = sc.Description + " Over a " + v.label + " session."
			c.Request = with(sc.Request)
			if id := c.Request["ClOrdID"]; id != "" && id != "ORD-USED" {
				c.Request["ClOrdID"] = id + "-" + strings.ToUpper(v.id)
			}
			out = append(out, c)
		}
	}
	return out
}

// fixFieldFamily: order quantities and prices a venue must refuse.
func fixFieldFamily() []Scenario {
	var out []Scenario
	bp := kv("availableBalance", "1000000.00")
	order := func(id, qty, price string) map[string]string {
		return map[string]string{"Account": "ACC1", "Symbol": "ACME", "Side": "buy", "OrderQty": qty, "Price": price, "Currency": "USD", "ClOrdID": id}
	}
	for _, q := range []struct{ id, qty, why string }{{"negative", "-1", "negative"}, {"fractional", "1.5", "not a whole number"}, {"huge", "1000000000000000000000", "beyond 18 digits"}} {
		out = append(out, Scenario{ID: "fix-qty-" + q.id, Name: "FIX D — order quantity " + q.qty, Protocol: "fix",
			Description: "Send an order for " + q.qty + " shares, which is " + q.why + ". The venue must reject with OrdRejReason=13 (incorrect quantity).",
			Request:     order("ORD-QTY-"+strings.ToUpper(q.id), q.qty, "10.00"), Context: with(bp, "expectReason", "13"), Expect: "reject (OrdRejReason=13)"})
	}
	for _, p := range []struct{ id, price, why string }{{"zero", "0", "zero"}, {"negative", "-1.00", "negative"}, {"text", "ten", "not a number"}} {
		out = append(out, Scenario{ID: "fix-price-" + p.id, Name: "FIX D — limit price " + p.price, Protocol: "fix",
			Description: "Send a limit order priced " + p.price + ", which is " + p.why + ". The venue must reject it (OrdRejReason=99).",
			Request:     order("ORD-PX-"+strings.ToUpper(p.id), "5", p.price), Context: with(bp, "expectReason", "99"), Expect: "reject (OrdRejReason=99)"})
	}
	return out
}
