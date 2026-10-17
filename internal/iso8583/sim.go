package iso8583

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sim is a built-in simulated card switch. It answers 0100 authorisations and 0200 financial
// transactions (0110/0210), 0400 reversals (0410) and 0800 network management (0810), deciding the DE39
// response code against a per-PAN ledger, the cards' states and the transactions it has approved.
// Modes let prova be shown catching a defect:
//
//	""/"compliant"   — every control below, with the conventional DE39 codes.
//	"approve-all"    — DE39=00 always (bug: approves over the limit, unknown reversals, bad cards).
//	"wrong-code"     — declines over-limit but with DE39=05 (do not honour) instead of 51.
type Sim struct {
	// Ledger maps PAN -> available balance in minor units.
	Ledger map[string]int64
	Mode   string
	// CardStatus maps a PAN to "lost" (41), "stolen" (43) or "blocked" (62, restricted card).
	CardStatus map[string]string
	// Limit maps a PAN to its per-transaction limit in minor units (61 above it).
	Limit map[string]int64
	// Approved maps the STAN of an approved transaction to its amount: what a reversal refers to.
	Approved map[string]int64
	// ProcessingDate (YYYY-MM-DD) is the date cards are checked for expiry against; empty means today.
	ProcessingDate string

	mu   sync.Mutex
	seen map[string]bool // STANs declared already processed (duplicates, 94)
}

// MarkSeen declares a STAN already processed, so a resubmission of it is declined as a duplicate.
func (s *Sim) MarkSeen(stan string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[stan] = true
}

// Decide computes the DE39 response code for an 0100 authorisation request.
func (s *Sim) Decide(req map[int]string) string { return s.DecideMTI("0100", req) }

// DecideMTI computes the DE39 response code for any request MTI.
func (s *Sim) DecideMTI(mti string, req map[int]string) string {
	if s.Mode == "approve-all" {
		return "00"
	}
	amt, _ := strconv.ParseInt(strings.TrimLeft(req[4], "0"), 10, 64)
	switch mti {
	case "0800":
		switch req[70] {
		case "001", "002", "301":
			return "00"
		}
		return "30" // format error: unknown network management code
	case "0400":
		stan := ""
		if len(req[90]) >= 10 {
			stan = req[90][4:10]
		}
		s.mu.Lock()
		orig, ok := s.Approved[stan]
		s.mu.Unlock()
		switch {
		case !ok:
			return "25" // unable to locate the original
		case amt > orig:
			return "13" // invalid amount: more than was approved
		}
		return "00"
	}

	pan := req[2]
	s.mu.Lock()
	avail := s.Ledger[pan]
	status := s.CardStatus[pan]
	limit, limited := s.Limit[pan]
	dup := s.seen[req[11]]
	s.mu.Unlock()
	short := amt > avail

	if s.Mode == "wrong-code" {
		if short {
			return "05"
		}
		return "00"
	}
	pd := s.ProcessingDate
	if pd == "" {
		pd = time.Now().UTC().Format("2006-01-02")
	}
	switch {
	case !luhn(pan):
		return "14" // invalid card number
	case req[14] != "" && req[14] < pd[2:4]+pd[5:7]:
		return "54" // expired card
	case status == "lost":
		return "41"
	case status == "stolen":
		return "43"
	case status == "blocked":
		return "62" // restricted card
	case dup:
		return "94" // duplicate transmission
	case amt <= 0:
		return "13" // invalid amount
	case limited && amt > limit:
		return "61" // exceeds amount limit
	case short:
		return "51" // insufficient funds
	}
	return "00"
}

func luhn(pan string) bool {
	if len(pan) < 12 || len(pan) > 19 {
		return false
	}
	sum := 0
	for i := 0; i < len(pan); i++ {
		d := int(pan[len(pan)-1-i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if i%2 == 1 {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// Serve listens on addr and handles framed 0100/0110 exchanges until ctx is cancelled.
func (s *Sim) Serve(ctx context.Context, addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("iso8583 sim: listen: %w", err)
	}
	go func() { <-ctx.Done(); _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
	return ln.Addr().String(), nil
}

func (s *Sim) handle(c net.Conn) {
	defer c.Close()
	for {
		raw, err := readFrame(c)
		if err != nil {
			return
		}
		mti, fields, err := Unpack(raw)
		if err != nil {
			return
		}
		out, err := EncodeResponse(mti, fields, s.DecideMTI(mti, fields))
		if err != nil {
			return
		}
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}
