package fix

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
)

// Sim is a built-in simulated trading venue. It completes a Logon, answers TestRequests with
// Heartbeats, and answers each order message: a NewOrderSingle with a fill or a rejection, an
// OrderCancelRequest or OrderCancelReplaceRequest with an ExecutionReport or an OrderCancelReject, and
// an OrderStatusRequest with a status report. It decides against per-account buying power and
// positions, the instruments it lists, and the orders it holds. Its state never changes, so every
// scenario can be repeated. Modes let prova be shown catching a defect:
//
//	""/"compliant"  — every control below, with FIX 4.4 OrdRejReason and CxlRejReason codes.
//	"approve-all"   — fills, cancels and replaces everything and reports every order filled (bug).
//	"wrong-reason"  — reject over-limit orders but with OrdRejReason=1 (unknown symbol) instead of 3.
type Sim struct {
	// Buying maps account id -> available buying power in minor units.
	Buying map[string]int64
	Mode   string
	// Positions maps "account/symbol" to the quantity held; a sale beyond it is rejected (3).
	Positions map[string]int64
	// Listed is the set of symbols the venue trades; nil lists every symbol. Others are rejected (1).
	Listed map[string]bool
	// Halted is the set of symbols whose market is closed (2).
	Halted map[string]bool
	// Partial is the set of symbols the venue only partially fills on a NewOrderSingle (ExecType=1).
	Partial map[string]bool
	// Orders are the orders the venue holds, by ClOrdID: what a cancel, replace or status request
	// refers to.
	Orders map[string]Order

	mu   sync.Mutex
	exec int
	seen map[string]bool // ClOrdIDs already used (duplicates, 6)
}

// Order is an order the venue holds.
type Order struct {
	Account, Symbol string
	Status          string // OrdStatus: 0 new, 1 partially filled, 2 filled, 4 cancelled
}

// MarkSeen declares a ClOrdID already used, so a new order reusing it is rejected as a duplicate.
func (s *Sim) MarkSeen(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[id] = true
}

// Decide computes the ExecutionReport outcome for a NewOrderSingle: whether to fill, and if not, the
// OrdRejReason and human text.
func (s *Sim) Decide(order map[int]string) (fill bool, rejReason, text string) {
	notional := notionalMinor(order[TagPrice], order[TagOrderQty])
	qty, _ := strconv.ParseInt(order[TagOrderQty], 10, 64)
	sym := order[TagSymbol]
	s.mu.Lock()
	power := s.Buying[order[TagAccount]]
	pos, havePos := s.Positions[order[TagAccount]+"/"+sym]
	listed := s.Listed == nil || s.Listed[sym]
	halted := s.Halted[sym]
	dup := s.seen[order[TagClOrdID]]
	s.mu.Unlock()
	buy := order[TagSide] != "2"
	over := buy && notional > power

	switch s.Mode {
	case "approve-all":
		return true, "", ""
	case "wrong-reason":
		if over {
			return false, "1", "unknown symbol"
		}
		return true, "", ""
	}
	switch {
	case !listed:
		return false, "1", "unknown symbol"
	case halted:
		return false, "2", "exchange closed"
	case qty <= 0:
		return false, "13", "incorrect quantity"
	case minorUnits(order[TagPrice]) <= 0:
		return false, "99", "price must be positive"
	case dup:
		return false, "6", "duplicate order"
	case !buy && havePos && qty > pos:
		return false, "3", "sale exceeds the position held"
	case over:
		return false, "3", "order notional exceeds buying power"
	}
	return true, "", ""
}

// respond answers a cancel (F), replace (G) or status (H) request.
func (s *Sim) respond(seq int, sender, target string, m map[int]string) []byte {
	s.mu.Lock()
	o, known := s.Orders[m[TagOrigClOrdID]]
	if m[TagMsgType] == MsgStatusRequest {
		o, known = s.Orders[m[TagClOrdID]]
	}
	power := s.Buying[m[TagAccount]]
	s.mu.Unlock()
	final := o.Status == "2" || o.Status == "4"
	t := now()
	switch m[TagMsgType] {
	case MsgCancelRequest:
		switch {
		case s.Mode == "approve-all":
		case !known:
			return CancelReject(seq, sender, target, t, m, "1", "1", "unknown order")
		case final:
			return CancelReject(seq, sender, target, t, m, "1", "0", "too late to cancel")
		}
		return ExecReport(seq, sender, target, t, m, s.nextExec(), "4", "4")
	case MsgReplaceRequest:
		switch {
		case s.Mode == "approve-all":
		case !known:
			return CancelReject(seq, sender, target, t, m, "2", "1", "unknown order")
		case final:
			return CancelReject(seq, sender, target, t, m, "2", "0", "too late to replace")
		case m[TagSide] != "2" && notionalMinor(m[TagPrice], m[TagOrderQty]) > power:
			return CancelReject(seq, sender, target, t, m, "2", "2", "replacement exceeds buying power")
		}
		return ExecReport(seq, sender, target, t, m, s.nextExec(), "5", "0")
	default: // MsgStatusRequest
		switch {
		case s.Mode == "approve-all":
			return ExecReport(seq, sender, target, t, m, s.nextExec(), "I", "2") // bug: every order "filled"
		case !known:
			return ExecReject(seq, sender, target, t, m, s.nextExec(), "5", "unknown order")
		}
		return ExecReport(seq, sender, target, t, m, s.nextExec(), "I", o.Status)
	}
}

// Serve listens on addr and handles FIX sessions until ctx is cancelled, returning the listen addr.
func (s *Sim) Serve(ctx context.Context, addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("fix sim: listen: %w", err)
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

func (s *Sim) nextExec() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exec++
	return fmt.Sprintf("%06d", s.exec)
}

func (s *Sim) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	seq := 1
	// The venue's comp ids are the client's, reversed; filled in on Logon.
	sender, target := "VENUE", "PROVA"
	for {
		raw, err := readMessage(r)
		if err != nil {
			return
		}
		m, err := parse(raw)
		if err != nil {
			return
		}
		switch m[TagMsgType] {
		case MsgLogon:
			sender, target = m[TagTargetCompID], m[TagSenderCompID]
			if sender == "" {
				sender = "VENUE"
			}
			if target == "" {
				target = "PROVA"
			}
			// Echo the client's session version (FIX.4.4 or FIXT.1.1 for FIX 5.0) and application version.
			bs := m[TagBeginString]
			if bs == "" {
				bs = beginString
			}
			if _, err := c.Write(LogonV(bs, m[TagDefaultApplVerID], seq, sender, target, now())); err != nil {
				return
			}
			seq++
		case MsgResendRequest:
			// Answer a resend request with an administrative SequenceReset in gap-fill mode, advancing
			// the peer past the requested range (prova's sim replays nothing application-level).
			bs := m[TagBeginString]
			if bs == "" {
				bs = beginString
			}
			end, _ := strconv.Atoi(m[TagEndSeqNo])
			if _, err := c.Write(SequenceResetGapFill(bs, seq, sender, target, now(), end+1)); err != nil {
				return
			}
			seq++
		case MsgTestRequest:
			if _, err := c.Write(Heartbeat(seq, sender, target, now(), m[TagTestReqID])); err != nil {
				return
			}
			seq++
		case MsgCancelRequest, MsgReplaceRequest, MsgStatusRequest:
			if _, err := c.Write(s.respond(seq, sender, target, m)); err != nil {
				return
			}
			seq++
		case MsgNewOrderSingle:
			fill, rej, text := s.Decide(m)
			var out []byte
			switch {
			case !fill:
				out = ExecReject(seq, sender, target, now(), m, s.nextExec(), rej, text)
			case s.Mode != "approve-all" && s.Partial[m[TagSymbol]]:
				// A partial fill: half the order quantity, the rest left working.
				q, _ := strconv.ParseInt(m[TagOrderQty], 10, 64)
				cum := q / 2
				if cum < 1 {
					cum = 1
				}
				out = ExecPartialFill(seq, sender, target, now(), m, s.nextExec(), cum)
			default:
				out = ExecFill(seq, sender, target, now(), m, s.nextExec())
			}
			if _, err := c.Write(out); err != nil {
				return
			}
			seq++
		default:
			// ignore anything else (Heartbeat, Logout, ...)
		}
	}
}
