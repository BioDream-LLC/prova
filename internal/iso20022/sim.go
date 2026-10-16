package iso20022

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sim is a built-in simulated payment host. It accepts pacs.008 credit transfers and answers with a
// pacs.002 status report, deciding accept/reject against an account ledger. prova scenarios run
// against it with no external system; it is also the fixture every test and demo targets.
//
// Behaviour is deliberately switchable so prova can be shown catching a real defect:
//
//	Mode "" / "compliant"       — rejects (AM04) when the debtor cannot cover the amount, else accepts.
//	Mode "accept-all" (buggy)   — accepts everything, including payments over the available balance.
//	Mode "wrong-reason" (buggy) — rejects insufficient-funds correctly but with the wrong reason code.
type Sim struct {
	// Ledger maps debtor account id -> available balance in minor units (cents).
	Ledger map[string]int64
	// Mode selects the host behaviour; empty means compliant.
	Mode string
	// BookCcy is the currency the host books in; a compliant host rejects (AM03) other currencies.
	// Empty means EUR.
	BookCcy string
	// SettlementDate is the host's processing date (ISO YYYY-MM-DD); a compliant host rejects (DT01)
	// an instruction value-dated before it. Empty means "today".
	SettlementDate string

	mu   sync.Mutex
	seen map[string]bool // EndToEndIds the tester has declared already processed
}

// MarkSeen declares an EndToEndId as already processed, so a compliant host rejects a resubmission
// of it as a duplicate. Only pre-seeded ids are treated as duplicates — ordinary traffic (including
// a repeated load scenario) is never spuriously rejected.
func (s *Sim) MarkSeen(e2e string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[e2e] = true
}

// Decide computes the status the host returns for a request. Exported so tests and the engine can
// evaluate the host logic without a socket.
func (s *Sim) Decide(req Request) Status {
	s.mu.Lock()
	avail := s.Ledger[req.DebtorAcct]
	dup := s.seen[req.EndToEndId]
	s.mu.Unlock()

	resp := Status{MsgId: "RSP-" + req.MsgId, OrgnlEndToEndId: req.EndToEndId}
	amt := minorUnits(req.Amount)
	short := amt > avail

	switch s.Mode {
	case "accept-all":
		resp.TxSts = "ACSP" // bug: ignores every control
		return resp
	case "wrong-reason":
		if short {
			resp.TxSts, resp.ReasonCode = "RJCT", "AC04" // bug: AC04 (closed account) instead of AM04
		} else {
			resp.TxSts = "ACSP"
		}
		return resp
	}

	// compliant: enforce controls in order, then funds.
	bookCcy := s.BookCcy
	if bookCcy == "" {
		bookCcy = "EUR"
	}
	settle := s.SettlementDate
	if settle == "" {
		settle = time.Now().UTC().Format("2006-01-02")
	}
	switch {
	case req.Currency != "" && !strings.EqualFold(req.Currency, bookCcy):
		resp.TxSts, resp.ReasonCode = "RJCT", "AM03" // unsupported currency
	case dup:
		resp.TxSts, resp.ReasonCode = "RJCT", "AM05" // duplication
	case req.ValueDate != "" && req.ValueDate < settle:
		resp.TxSts, resp.ReasonCode = "RJCT", "DT01" // invalid date (back-valued)
	case short:
		resp.TxSts, resp.ReasonCode = "RJCT", "AM04" // insufficient funds
	default:
		resp.TxSts = "ACSP"
	}
	return resp
}

// Serve listens on addr and handles framed pacs.008/pacs.002 exchanges until the context is
// cancelled. It returns the actual listen address (useful when addr uses port 0).
func (s *Sim) Serve(ctx context.Context, addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("iso20022 sim: listen: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
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
		req, err := ParseRequest(raw)
		if err != nil {
			return
		}
		out, err := EncodeStatus(s.Decide(req))
		if err != nil {
			return
		}
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}

// minorUnits parses a decimal amount string like "150.00" into integer minor units (15000).
// It is tolerant of missing or short fractional parts and ignores a leading sign.
func minorUnits(s string) int64 {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt(frac, 10, 64)
	v := w*100 + f
	if neg {
		v = -v
	}
	return v
}
