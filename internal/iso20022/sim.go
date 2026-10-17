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

// Sim is a built-in simulated payment host. It accepts pacs.008 and pacs.009 credit transfers, pacs.004
// returns, camt.056 cancellation requests and pacs.028 status requests, and answers each with a pacs.002
// status report (or, for a cancellation, a camt.029 resolution), deciding against an account ledger and
// the originals it has processed. prova scenarios run against it with no external system; it is also
// the fixture every test and demo targets.
//
// Behaviour is deliberately switchable so prova can be shown catching a real defect:
//
//	Mode "" / "compliant"       — enforces every control below with the ISO external reason codes.
//	Mode "accept-all" (buggy)   — accepts everything: over-balance payments, invalid instructions,
//	                              returns of unknown originals, cancellations of final ones.
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
	// AccountStatus maps an account id to "closed" (AC04) or "blocked" (AC06).
	AccountStatus map[string]string
	// Limit maps an account id to its per-transaction limit in minor units (AM02 above it).
	Limit map[string]int64
	// Originals are the instructions the host has already processed, by EndToEndId: what a return,
	// cancellation or status request refers to.
	Originals map[string]Original

	mu   sync.Mutex
	seen map[string]bool // EndToEndIds the tester has declared already processed
}

// Original is an instruction the host has processed.
type Original struct {
	Amount int64  // minor units
	Status string // as a status report states it: ACSC (settled), PDNG (pending) ...
	// Returned marks an original whose funds have already been sent back.
	Returned bool
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
	resp := Status{Kind: KindPacs002, MsgId: "RSP-" + req.MsgId, OrgnlEndToEndId: req.EndToEndId}
	reject := func(code string) Status { resp.TxSts, resp.ReasonCode = "RJCT", code; return resp }
	accept := func() Status { resp.TxSts = "ACSP"; return resp }

	s.mu.Lock()
	orig, known := s.Originals[req.OrgnlEndToEndId]
	s.mu.Unlock()

	switch req.Kind {
	case KindCamt056:
		resp.Kind, resp.OrgnlEndToEndId = KindCamt029, req.OrgnlEndToEndId
		switch {
		case s.Mode == "accept-all":
			resp.TxSts = "CNCL" // bug: cancels whatever it is asked to
		case !known:
			resp.TxSts, resp.ReasonCode = "RJCR", "NOOR" // no original transaction received
		case orig.Returned:
			resp.TxSts, resp.ReasonCode = "RJCR", "ARDT" // already returned
		case orig.Status != "PDNG":
			resp.TxSts, resp.ReasonCode = "RJCR", "CUST" // settled: only the creditor can agree to return it
		default:
			resp.TxSts = "CNCL"
		}
		return resp
	case KindPacs028:
		resp.OrgnlEndToEndId = req.OrgnlEndToEndId
		switch {
		case s.Mode == "accept-all":
			resp.TxSts = "ACSC" // bug: reports every payment settled
		case !known:
			return reject("NOOR")
		default:
			resp.TxSts = orig.Status
		}
		return resp
	case KindPacs004:
		resp.OrgnlEndToEndId = req.OrgnlEndToEndId
		switch {
		case s.Mode == "accept-all":
			return accept()
		case !validAmount(req.Amount, req.Currency):
			return reject("AM12")
		case !known:
			return reject("NOOR")
		case orig.Returned:
			return reject("AM05") // a second return of the same payment
		case minorUnits(req.Amount) > orig.Amount:
			return reject("AM09") // wrong amount: more than was paid
		}
		return accept()
	}

	// A credit transfer: pacs.008 or pacs.009.
	s.mu.Lock()
	avail := s.Ledger[req.DebtorAcct]
	dup := s.seen[req.EndToEndId]
	status := s.AccountStatus[req.DebtorAcct]
	limit, limited := s.Limit[req.DebtorAcct]
	s.mu.Unlock()
	amount := minorUnits(req.Amount)
	short := amount > avail

	switch s.Mode {
	case "accept-all":
		return accept() // bug: ignores every control
	case "wrong-reason":
		if short {
			return reject("AC04") // bug: AC04 (closed account) instead of AM04
		}
		return accept()
	}

	// compliant: validate the message, then enforce the controls in order, then funds.
	bookCcy := s.BookCcy
	if bookCcy == "" {
		bookCcy = "EUR"
	}
	settle := s.SettlementDate
	if settle == "" {
		settle = time.Now().UTC().Format("2006-01-02")
	}
	switch {
	case !knownCurrency(req.Currency):
		return reject("AM11") // invalid transaction currency
	case !validAmount(req.Amount, req.Currency):
		if amount == 0 {
			return reject("AM01") // zero amount
		}
		return reject("AM12") // invalid amount
	case !validBIC(req.DebtorBIC) || !validBIC(req.CreditBIC):
		return reject("RC01") // bank identifier incorrect
	case req.DebtorIBAN != "" && !validIBAN(req.DebtorIBAN), req.CreditIBAN != "" && !validIBAN(req.CreditIBAN):
		return reject("AC01") // incorrect account number
	case status == "closed":
		return reject("AC04")
	case status == "blocked":
		return reject("AC06")
	case req.Currency != "" && !strings.EqualFold(req.Currency, bookCcy):
		return reject("AM03") // not allowed currency
	case dup:
		return reject("AM05") // duplication
	case req.ValueDate != "" && req.ValueDate < settle:
		return reject("DT01") // invalid date (back-valued)
	case limited && amount > limit:
		return reject("AM02") // not allowed amount: over the transaction limit
	case short:
		return reject("AM04") // insufficient funds
	}
	return accept()
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
