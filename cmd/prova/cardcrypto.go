package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/cardcrypto"
	"github.com/biodream-llc/prova/internal/hsm"
	"github.com/biodream-llc/prova/internal/testkeys"
)

// cardCryptoRequest is one calculation on the Card security screen. Keys are test keys: prova is a
// test harness and never holds production keys.
type cardCryptoRequest struct {
	Op        string `json:"op"` // pinblock | decipher | mac | arqc | verify-arqc | pvv | cvv | kcv
	Format    string `json:"format"`
	Key       string `json:"key"`
	PIN       string `json:"pin"`
	PAN       string `json:"pan"`
	PSN       string `json:"psn"`
	Block     string `json:"block"`
	Algorithm string `json:"algorithm"`
	Data      string `json:"data"` // hex; for a MAC, empty means prova's MAC fields below
	ProcCode  string `json:"processingCode"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	STAN      string `json:"stan"`
	Terminal  string `json:"terminal"`
	Version   string `json:"version"`
	ICC       string `json:"icc"`
	ATC       string `json:"atc"`
	UN        string `json:"un"`
	Date      string `json:"date"`
	PVKI      string `json:"pvki"`
	Expiry    string `json:"expiry"`
	Service   string `json:"service"`
}

func cardCrypto(q cardCryptoRequest) (map[string]any, error) {
	key, err := cardcrypto.Hex(q.Key)
	if err != nil && q.Op != "verify-arqc" {
		return nil, fmt.Errorf("key: %w", err)
	}
	switch q.Op {
	case "pinblock":
		f, err := cardcrypto.ParseFormat(q.Format)
		if err != nil {
			return nil, err
		}
		b, err := cardcrypto.EncipherPINBlock(f, key, q.PIN, q.PAN)
		if err != nil {
			return nil, err
		}
		return map[string]any{"block": cardcrypto.HexUpper(b), "format": f}, nil
	case "decipher":
		f, err := cardcrypto.ParseFormat(q.Format)
		if err != nil {
			return nil, err
		}
		b, err := cardcrypto.Hex(q.Block)
		if err != nil {
			return nil, err
		}
		pin, err := cardcrypto.DecipherPIN(f, key, b, q.PAN)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pin": pin}, nil
	case "mac":
		data := cardcrypto.MACData(q.PAN, orDefault(q.ProcCode, "000000"), q.Amount, q.Currency, q.STAN, q.Terminal)
		if q.Data != "" {
			if data, err = cardcrypto.Hex(q.Data); err != nil {
				return nil, err
			}
		}
		m, err := cardcrypto.MAC(q.Algorithm, key, data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"mac": cardcrypto.HexUpper(m), "mac8": cardcrypto.HexUpper(m[:8]), "input": cardcrypto.HexUpper(data)}, nil
	case "arqc":
		amt := int64(0)
		if q.Amount != "" {
			whole, frac, _ := strings.Cut(q.Amount, ".")
			w, _ := strconv.ParseInt(whole, 10, 64)
			f, _ := strconv.ParseInt((frac + "00")[:2], 10, 64)
			amt = w*100 + f
		}
		icc, err := cardcrypto.BuildChipData(cardcrypto.ChipData{Version: orDefault(q.Version, cardcrypto.VisaCVN10), IMK: key, PAN: q.PAN,
			PSN: orDefault(q.PSN, "00"), Amount: amt, Currency: orDefault(q.Currency, "978"), Country: "276",
			Date: orDefault(q.Date, time.Now().UTC().Format("060102")), UN: orDefault(q.UN, "1A2B3C4D"), ATC: orDefault(q.ATC, "0001")})
		if err != nil {
			return nil, err
		}
		objs, _ := cardcrypto.ParseTLV(icc)
		return map[string]any{"icc": icc, "arqc": cardcrypto.HexUpper(objs[0].Value)}, nil
	case "verify-arqc":
		k, err := cardcrypto.Hex(q.Key)
		if err != nil {
			return nil, err
		}
		ok, err := cardcrypto.VerifyARQC(orDefault(q.Version, cardcrypto.VisaCVN10), k, q.PAN, q.PSN, q.ICC)
		if err != nil {
			return nil, err
		}
		return map[string]any{"valid": ok}, nil
	case "pvv":
		v, err := cardcrypto.PVV(key, q.PAN, orDefault(q.PVKI, "1"), q.PIN)
		if err != nil {
			return nil, err
		}
		return map[string]any{"pvv": v}, nil
	case "cvv":
		v, err := cardcrypto.CVV(key, q.PAN, q.Expiry, orDefault(q.Service, "101"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"cvv": v}, nil
	case "kcv":
		v, err := cardcrypto.KCV(key)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kcv": v}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", q.Op)
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// registerCardCrypto adds the card-security calculator and the HSM check. The HSM is the one named by
// -hsm when the server started; requests cannot point prova at other addresses.
func registerCardCrypto(mux *http.ServeMux, hsmAddr, hsmHeader string) {
	mux.HandleFunc("GET /api/cardcrypto/testkeys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"pinKey": testkeys.PINKey, "pinKeyAES": testkeys.PINKeyAES, "macKey": testkeys.MACKey, "macKeyAES": testkeys.MACKeyAES,
			"imk": testkeys.IMK, "cards": testkeys.Cards, "versions": cardcrypto.CryptogramVersions(),
		})
	})
	mux.HandleFunc("POST /api/cardcrypto", func(w http.ResponseWriter, r *http.Request) {
		var q cardCryptoRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&q); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		out, err := cardCrypto(q)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /api/hsm", func(w http.ResponseWriter, r *http.Request) {
		if hsmAddr == "" {
			writeJSON(w, http.StatusOK, map[string]any{"configured": false})
			return
		}
		c := &hsm.Client{Addr: hsmAddr, Header: hsmHeader, Timeout: 5 * time.Second}
		lmk, fw, err := c.Diagnostics(r.Context())
		out := map[string]any{"configured": true, "addr": hsmAddr}
		if err != nil {
			out["error"] = err.Error()
		} else {
			out["lmkCheck"], out["firmware"] = lmk, fw
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// hsmCLI is `prova hsm`: it checks an HSM that speaks the payShield host commands.
//
//	prova hsm --addr 10.0.0.5:1500 [--header SSSS] diagnostics
//	prova hsm --addr ... verify-pin  ZPK PVK-PAIR PIN-BLOCK FORMAT PAN PVKI PVV
//	prova hsm --addr ... verify-cvv  CVK CVV PAN EXPIRY SERVICE-CODE
//	prova hsm --addr ... translate-pin TPK ZPK PIN-BLOCK PAN
func hsmCLI(args []string) int {
	fs := flag.NewFlagSet("prova hsm", flag.ContinueOnError)
	addr := fs.String("addr", "", "HSM host:port")
	header := fs.String("header", "", "message header the HSM is configured with")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *addr == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: prova hsm --addr host:port [--header H] diagnostics | verify-pin ... | verify-cvv ... | translate-pin ...")
		return 2
	}
	c := &hsm.Client{Addr: *addr, Header: *header}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := fs.Args()
	need := func(n int) bool {
		if len(a) != n+1 {
			fmt.Fprintf(os.Stderr, "prova hsm %s takes %d arguments\n", a[0], n)
			return false
		}
		return true
	}
	verdict := func(ok bool, err error) int {
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova hsm:", err)
			return 1
		}
		if ok {
			fmt.Println("verified")
			return 0
		}
		fmt.Println("verification failure")
		return 1
	}
	switch a[0] {
	case "diagnostics", "nc":
		lmk, fw, err := c.Diagnostics(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova hsm:", err)
			return 1
		}
		fmt.Printf("LMK check value %s, firmware %s\n", lmk, fw)
		return 0
	case "verify-pin":
		if !need(7) {
			return 2
		}
		f, err := cardcrypto.ParseFormat(a[4])
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova hsm:", err)
			return 2
		}
		return verdict(c.VerifyPINWithPVV(ctx, a[1], a[2], a[3], f, a[5], a[6], a[7]))
	case "verify-cvv":
		if !need(5) {
			return 2
		}
		return verdict(c.VerifyCVV(ctx, a[1], a[2], a[3], a[4], a[5]))
	case "translate-pin":
		if !need(4) {
			return 2
		}
		n, block, err := c.TranslatePIN(ctx, a[1], a[2], 12, a[3], 0, 0, a[4])
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova hsm:", err)
			return 1
		}
		fmt.Printf("PIN length %d, block under the destination key %s\n", n, block)
		return 0
	}
	fmt.Fprintf(os.Stderr, "prova hsm: unknown command %q\n", a[0])
	return 2
}
