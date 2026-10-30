package qcasebook

import (
	"fmt"
	"testing"

	"github.com/biodream-llc/prova/internal/cardcrypto"
	"github.com/biodream-llc/prova/internal/verdict"
)

// TestSecurityAgrees compares the two engines' separately written card-security checks: PIN blocks of
// every format under right and wrong keys and PINs, MACs of every algorithm over right and altered
// fields, and cryptograms of every version under right and wrong issuer keys and tampered chip data,
// each crossed with acceptance and rejection. It also covers mandates, session rules and account
// reports.
func TestSecurityAgrees(t *testing.T) {
	q, g := Default(), verdict.Default()
	n, bad := 0, 0
	compare := func(req, ctx map[string]string) {
		for _, resp := range []map[string]string{{"decision": "accepted"}, {"decision": "rejected", "rejectReason": "55"}, {"decision": "rejected"}} {
			c := verdict.Case{Request: req, Context: ctx, Response: resp}
			n++
			if gq, gg := q.Prove(c).Outcome, g.Prove(c).Outcome; gq != gg {
				bad++
				if bad <= 10 {
					t.Errorf("disagree: quorum=%s go=%s case=%v", gq, gg, c)
				}
			}
		}
	}
	h := cardcrypto.HexUpper
	pan := "4111111111111111"
	base := func() map[string]string {
		return map[string]string{"PAN": pan, "ProcessingCode": "000000", "Amount": "90.00", "Currency": "EUR", "STAN": "000120", "Terminal": "TERM0001"}
	}
	keys := map[int][]string{
		0: {"0123456789ABCDEFFEDCBA9876543210", "0123456789ABCDEF0123456789ABCDEFFEDCBA9876543210", "0123456789ABCDEF"},
		1: {"0123456789ABCDEFFEDCBA9876543210"},
		3: {"0123456789ABCDEFFEDCBA9876543210"},
		4: {"00112233445566778899AABBCCDDEEFF", "00112233445566778899AABBCCDDEEFF0011223344556677", "00112233445566778899AABBCCDDEEFF00112233445566778899AABBCCDDEEFF"},
	}
	for f, ks := range keys {
		for _, kh := range ks {
			k, _ := cardcrypto.Hex(kh)
			for _, pin := range []string{"1234", "98765", "123456789012"} {
				blk, err := cardcrypto.EncipherPINBlock(f, k, pin, pan)
				if err != nil {
					t.Fatalf("format %d key %d: %v", f, len(k), err)
				}
				for _, cardPIN := range []string{pin, "0000"} {
					for _, keySeed := range []string{kh, keys[f][0][:len(keys[f][0])-2] + "00", "ZZ"} {
						for _, fmtSeed := range []string{fmt.Sprint(f), "", "2"} {
							req := base()
							req["PINBlock"] = h(blk)
							compare(req, map[string]string{"pinKey": keySeed, "cardPIN": cardPIN, "pinFormat": fmtSeed, "debit": "false"})
							req2 := base()
							req2["PINBlock"] = h(blk[:len(blk)-1]) + "00"
							compare(req2, map[string]string{"pinKey": keySeed, "cardPIN": cardPIN, "pinFormat": fmtSeed, "debit": "false"})
						}
					}
				}
			}
		}
	}
	for _, alg := range []string{"", "retail", "cbc", "cmac", "bogus"} {
		for _, kh := range []string{"0123456789ABCDEFFEDCBA9876543210", "00112233445566778899AABBCCDDEEFF00112233445566778899AABBCCDDEEFF", "0123456789ABCDEF"} {
			k, _ := cardcrypto.Hex(kh)
			req := base()
			mac, err := cardcrypto.MAC(alg, k, cardcrypto.MACData(req["PAN"], req["ProcessingCode"], req["Amount"], req["Currency"], req["STAN"], req["Terminal"]))
			macs := []string{"A1B2C3D4E5F60718"}
			if err == nil {
				macs = append(macs, h(mac), h(mac)[:16], "A1B2", h(mac)+"00")
			}
			for _, m := range macs {
				for _, amt := range []string{"90.00", "91.00"} {
					r := base()
					r["MAC"], r["Amount"] = m, amt
					compare(r, map[string]string{"macKey": kh, "macAlgorithm": alg, "debit": "false"})
				}
			}
		}
	}
	imk, _ := cardcrypto.Hex("0123456789ABCDEFFEDCBA9876543210")
	for _, ver := range append(cardcrypto.CryptogramVersions(), "") {
		build := ver
		if build == "" {
			build = cardcrypto.VisaCVN10
		}
		for _, p := range []string{pan, "5555555555554444", "4000123412341234567"} {
			icc, err := cardcrypto.BuildChipData(cardcrypto.ChipData{Version: build, IMK: imk, PAN: p, PSN: "01", Amount: 9000,
				Currency: "978", Country: "276", Date: "261015", UN: "1A2B3C4D", ATC: "0042"})
			if err != nil {
				t.Fatal(err)
			}
			objs, _ := cardcrypto.ParseTLV(icc)
			tamper := func(tag, val string) string {
				out := append([]cardcrypto.TLV(nil), objs...)
				for i := range out {
					if out[i].Tag == tag {
						v, _ := cardcrypto.Hex(val)
						out[i].Value = v
					}
				}
				return cardcrypto.BuildTLV(out)
			}
			for _, data := range []string{icc, tamper("9F02", "000000009001"), tamper("9F36", "0043"), tamper("9F36", "004300"),
				tamper("9F37", "1A2B3C"), tamper("5F34", "02"), tamper("9F10", "0601"), tamper("9F26", "0000000000000000"), icc[:len(icc)-2], "9F2600"} {
				for _, ks := range []string{"0123456789ABCDEFFEDCBA9876543210", "0123456789ABCDEFFEDCBA9876543211", "0123456789ABCDEF"} {
					for _, vs := range []string{ver, cardcrypto.VisaCVN18, "nope"} {
						r := base()
						r["PAN"], r["ICCData"] = p, data
						compare(r, map[string]string{"imk": ks, "cryptogramVersion": vs, "debit": "false"})
					}
				}
			}
		}
	}
	for _, mv := range []string{"", "false", "true"} {
		for _, sv := range []string{"", "true"} {
			for _, rep := range []map[string]string{{}, {"statementBalance": "142.00"}, {"statementAccount": "ACC-EUR-1", "statementEntries": "2"}} {
				ctx := map[string]string{"debit": "false", "mandateValid": mv, "sessionViolation": sv, "expectReason": "MD01"}
				for k, v := range rep {
					ctx[k] = v
				}
				for _, resp := range []map[string]string{
					{"decision": "accepted", "reportedBalance": "142.00", "reportedAccount": "ACC-EUR-1", "entries": "2"},
					{"decision": "accepted", "reportedBalance": "242.00", "reportedAccount": "ACC-OTHER", "entries": "0"},
					{"decision": "accepted"}, {"decision": "rejected", "rejectReason": "MD01"}, {"decision": "rejected", "rejectReason": "AM04"},
				} {
					c := verdict.Case{Request: map[string]string{"Amount": "90.00"}, Context: ctx, Response: resp}
					n++
					if gq, gg := q.Prove(c).Outcome, g.Prove(c).Outcome; gq != gg {
						bad++
						if bad <= 10 {
							t.Errorf("disagree: quorum=%s go=%s case=%v", gq, gg, c)
						}
					}
				}
			}
		}
	}
	t.Logf("%d card-security, mandate, session and report cases compared, %d disagreements", n, bad)
}
