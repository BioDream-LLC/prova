// Package sims starts prova's built-in simulated targets: payment hosts, a card switch and trading
// venues, each seeded with the accounts, cards, originals and orders the shipped scenarios refer to. A
// compliant target of each protocol shows prova proving correct behaviour; a defective one shows it
// catching a fault. The server, the command line and the tests all start the same set.
package sims

import (
	"context"
	"fmt"

	"github.com/biodream-llc/prova/internal/cardcrypto"
	"github.com/biodream-llc/prova/internal/fix"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/iso8583"
	"github.com/biodream-llc/prova/internal/testkeys"
)

// Target is a running simulated target.
type Target struct {
	Name, Protocol, Note, Addr string
	Params                     map[string]string
}

// Spec describes a simulated target before it starts.
type Spec struct {
	Name, Protocol, Note string
	Params               map[string]string
}

// Specs lists the simulated targets in the order they are offered.
var Specs = []Spec{
	{"cbpr-uat", "iso20022", "compliant reference host", nil},
	{"switch-sim", "iso20022", "accept-all (known defect)", nil},
	{"card-switch", "iso8583", "compliant card switch", nil},
	{"card-switch-bug", "iso8583", "approves everything (known defect)", nil},
	{"visa-switch", "iso8583", "compliant switch, Visa-style dialect (BCD, EBCDIC, binary bitmaps, 22-byte header)", map[string]string{"dialect": "visa"}},
	{"mc-switch", "iso8583", "compliant switch, Mastercard-style dialect (EBCDIC, binary bitmaps)", map[string]string{"dialect": "mastercard"}},
	{"fix-venue", "fix", "compliant trading venue", nil},
	{"fix-venue-bug", "fix", "fills, cancels and replaces everything (known defect)", nil},
}

// The seeded state the shipped scenarios rely on.
const (
	IBANDebtor   = "DE89370400440532013000"
	IBANCreditor = "FR1420041010050500013M02606"
)

func paymentHost(mode string) *iso20022.Sim {
	s := &iso20022.Sim{
		Mode: mode,
		Ledger: map[string]int64{
			"ACC-EUR-1": 14200, IBANDebtor: 14200,
			"ACC-EUR-CLOSED": 100000, "ACC-EUR-BLOCKED": 100000, "ACC-EUR-LTD": 1000000,
		},
		AccountStatus: map[string]string{"ACC-EUR-CLOSED": "closed", "ACC-EUR-BLOCKED": "blocked"},
		Limit:         map[string]int64{"ACC-EUR-LTD": 50000},
		Originals: map[string]iso20022.Original{
			"E2E-ORIG-SETTLED":  {Amount: 9000, Status: "ACSC"},
			"E2E-ORIG-PENDING":  {Amount: 9000, Status: "PDNG"},
			"E2E-ORIG-RETURNED": {Amount: 9000, Status: "ACSC", Returned: true},
		},
	}
	s.MarkSeen("E2E-DUP") // the duplicate-instruction scenarios resubmit these ids
	s.MarkSeen("E2E-DUP-009")
	return s
}

// issuerKeys are the switch's keys and cards: the published test keys in internal/testkeys.
func issuerKeys() *iso8583.IssuerKeys {
	k := func(h string) []byte { b, _ := cardcrypto.Hex(h); return b }
	ik := &iso8583.IssuerKeys{
		PINKey: k(testkeys.PINKey), PINKeyAES: k(testkeys.PINKeyAES),
		MACKey: k(testkeys.MACKey), MACKeyAES: k(testkeys.MACKeyAES), IMK: k(testkeys.IMK),
		PINs: map[string]string{}, Cryptogram: map[string]string{},
	}
	for _, c := range testkeys.Cards {
		ik.PINs[c.PAN], ik.Cryptogram[c.PAN] = c.PIN, c.Cryptogram
	}
	return ik
}

func cardSwitch(mode string, dialect iso8583.Dialect) *iso8583.Sim {
	s := &iso8583.Sim{
		Mode:    mode,
		Dialect: dialect,
		Keys:    issuerKeys(),
		Ledger: map[string]int64{
			"4111111111111111": 14200, "5555555555554444": 100000,
			"4000000000000002": 100000, "4000000000000010": 100000,
			"4012888888881881": 14200, "5105105105105100": 14200,
		},
		CardStatus: map[string]string{"4000000000000002": "lost", "4000000000000010": "stolen"},
		Limit:      map[string]int64{"5555555555554444": 50000},
		Approved:   map[string]int64{"000201": 9000},
	}
	s.MarkSeen("000150")
	return s
}

func venue(mode string) *fix.Sim {
	s := &fix.Sim{
		Mode:      mode,
		Buying:    map[string]int64{"ACC1": 14200},
		Positions: map[string]int64{"ACC1/ACME": 20},
		Listed:    map[string]bool{"ACME": true, "GLOBX": true, "PARTL": true},
		Halted:    map[string]bool{"GLOBX": true},
		Partial:   map[string]bool{"PARTL": true},
		Orders: map[string]fix.Order{
			"ORD-OPEN":   {Account: "ACC1", Symbol: "ACME", Status: "0"},
			"ORD-FILLED": {Account: "ACC1", Symbol: "ACME", Status: "2"},
		},
	}
	s.MarkSeen("ORD-USED")
	return s
}

type server interface {
	Serve(ctx context.Context, addr string) (string, error)
}

func build(name string) server {
	switch name {
	case "cbpr-uat":
		return paymentHost("")
	case "switch-sim":
		return paymentHost("accept-all")
	case "card-switch":
		return cardSwitch("", iso8583.Generic)
	case "card-switch-bug":
		return cardSwitch("approve-all", iso8583.Generic)
	case "visa-switch":
		return cardSwitch("", iso8583.Visa)
	case "mc-switch":
		return cardSwitch("", iso8583.Mastercard)
	case "fix-venue":
		return venue("")
	case "fix-venue-bug":
		return venue("approve-all")
	}
	return nil
}

// Start launches every simulated target on a loopback port until ctx ends. A target that cannot
// start is reported in the error and left out; the others still run.
func Start(ctx context.Context) ([]Target, error) {
	var out []Target
	var firstErr error
	for _, sp := range Specs {
		addr, err := build(sp.Name).Serve(ctx, "127.0.0.1:0")
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("sims: %s: %w", sp.Name, err)
			}
			continue
		}
		out = append(out, Target{Name: sp.Name, Protocol: sp.Protocol, Note: sp.Note, Addr: addr, Params: sp.Params})
	}
	return out, firstErr
}

// Compliant names the compliant simulated target for a protocol.
func Compliant(protocol string) string {
	switch protocol {
	case "iso8583":
		return "card-switch"
	case "fix":
		return "fix-venue"
	}
	return "cbpr-uat"
}

// Defective names the defective simulated target for a protocol.
func Defective(protocol string) string {
	switch protocol {
	case "iso8583":
		return "card-switch-bug"
	case "fix":
		return "fix-venue-bug"
	}
	return "switch-sim"
}
