package fix

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
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

// session is one client's session state at the venue.
type session struct {
	bs, applVer    string // the client's BeginString and DefaultApplVerID
	sender, target string // the venue's comp ids (the client's, reversed)
	seq            int    // the venue's next outgoing MsgSeqNum
	expect         int    // the next MsgSeqNum the venue expects from the client
}

func (ss *session) out(msg []byte) []byte {
	ss.seq++
	return Rebrand(msg, ss.bs)
}

// handle runs one FIX session. A compliant venue enforces the session rules a counterparty may rely
// on: an incoming MsgSeqNum lower than expected ends the session (Logout) unless the message is a
// possible duplicate, which is then ignored; a possible duplicate must carry OrigSendingTime (Reject
// 373=1); comp ids must match the session's (Reject 373=9 and Logout); a message whose CheckSum fails is
// ignored, as garbled; a gap is answered by a ResendRequest. A defective venue checks none of it.
func (s *Sim) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	ss := &session{seq: 1, expect: 1, sender: "VENUE", target: "PROVA", bs: beginString}
	lax := s.Mode == "approve-all"
	write := func(msg []byte) bool { _, err := c.Write(ss.out(msg)); return err == nil }
	for {
		raw, err := readMessage(r)
		if err != nil {
			return
		}
		m, err := parse(raw)
		if err != nil {
			if lax { // the defective venue does not check the CheckSum at all
				m = parseLax(raw)
			} else {
				continue // garbled: ignore it and keep the session, as the session rules require
			}
		}
		seq, _ := strconv.Atoi(m[TagMsgSeqNum])
		t := now()
		if m[TagMsgType] == MsgLogon {
			ss.sender, ss.target = orDefault(m[TagTargetCompID], "VENUE"), orDefault(m[TagSenderCompID], "PROVA")
			ss.bs, ss.applVer = orDefault(m[TagBeginString], beginString), m[TagDefaultApplVerID]
			ss.expect = seq + 1
			if !write(LogonV(ss.bs, ss.applVer, ss.seq, ss.sender, ss.target, t)) {
				return
			}
			continue
		}
		if !lax {
			switch {
			case m[TagTargetCompID] != ss.sender || m[TagSenderCompID] != ss.target:
				write(SessionReject(ss.bs, ss.seq, ss.sender, ss.target, t, seq, strconv.Itoa(TagTargetCompID), m[TagMsgType], RejectCompIDProblem, "CompID problem"))
				write(Logout(ss.bs, ss.seq, ss.sender, ss.target, t, "CompID problem"))
				return
			case seq < ss.expect && m[TagPossDupFlag] != "Y":
				write(Logout(ss.bs, ss.seq, ss.sender, ss.target, t, fmt.Sprintf("MsgSeqNum too low, expecting %d but received %d", ss.expect, seq)))
				return
			case m[TagPossDupFlag] == "Y" && m[TagOrigSendingTime] == "":
				ss.expect = max(ss.expect, seq+1)
				if !write(SessionReject(ss.bs, ss.seq, ss.sender, ss.target, t, seq, strconv.Itoa(TagOrigSendingTime), m[TagMsgType], RejectRequiredTagMissing, "PossDupFlag=Y without OrigSendingTime")) {
					return
				}
				continue
			case seq < ss.expect:
				continue // a possible duplicate of a message already processed: ignore it
			case seq > ss.expect:
				// A gap: ask for the missing messages, then carry on with this one.
				if !write(ResendRequest(ss.bs, ss.seq, ss.sender, ss.target, t, ss.expect, seq-1)) {
					return
				}
			}
		}
		ss.expect = seq + 1
		var out []byte
		switch m[TagMsgType] {
		case MsgResendRequest:
			// Answer a resend request with an administrative SequenceReset in gap-fill mode, advancing
			// the peer past the requested range (prova's venue replays nothing application-level).
			end, _ := strconv.Atoi(m[TagEndSeqNo])
			out = SequenceResetGapFill(ss.bs, ss.seq, ss.sender, ss.target, t, end+1)
		case MsgSequenceReset:
			if n, err := strconv.Atoi(m[TagNewSeqNo]); err == nil {
				if n < ss.expect && m[TagGapFillFlag] != "Y" && !lax {
					out = SessionReject(ss.bs, ss.seq, ss.sender, ss.target, t, seq, strconv.Itoa(TagNewSeqNo), MsgSequenceReset, RejectValueIncorrect, "NewSeqNo is lower than expected")
				} else {
					ss.expect = n
				}
			}
		case MsgTestRequest:
			out = Heartbeat(ss.seq, ss.sender, ss.target, t, m[TagTestReqID])
		case MsgCancelRequest, MsgReplaceRequest, MsgStatusRequest:
			out = s.respond(ss.seq, ss.sender, ss.target, m)
		case MsgMassCancelRequest:
			if ss.bs == BeginString42 && !lax {
				// OrderMassCancelRequest arrived in FIX 4.3: a FIX 4.2 session has no such message.
				out = SessionReject(ss.bs, ss.seq, ss.sender, ss.target, t, seq, "", MsgMassCancelRequest, RejectInvalidMsgType, "Invalid MsgType for FIX.4.2")
			} else {
				out = s.massCancel(ss.seq, ss.sender, ss.target, m)
			}
		case MsgNewOrderSingle:
			fill, rej, text := s.Decide(m)
			switch {
			case !fill:
				out = ExecReject(ss.seq, ss.sender, ss.target, t, m, s.nextExec(), rej, text)
			case s.Mode != "approve-all" && s.Partial[m[TagSymbol]]:
				// A partial fill: half the order quantity, the rest left working.
				q, _ := strconv.ParseInt(m[TagOrderQty], 10, 64)
				out = ExecPartialFill(ss.seq, ss.sender, ss.target, t, m, s.nextExec(), max(q/2, 1))
			default:
				out = ExecFill(ss.seq, ss.sender, ss.target, t, m, s.nextExec())
			}
		}
		if out != nil && !write(out) {
			return
		}
	}
}

// massCancel answers an OrderMassCancelRequest: every open order (530=7), or every open order in one
// listed instrument (530=1). An unlisted instrument is refused with 532=1.
func (s *Sim) massCancel(seq int, sender, target string, m map[int]string) []byte {
	sym := m[TagSymbol]
	s.mu.Lock()
	defer s.mu.Unlock()
	if m[TagMassCancelRequestType] == "1" && s.Listed != nil && !s.Listed[sym] && s.Mode != "approve-all" {
		return MassCancelReport(seq, sender, target, now(), m, "0", "1", 0)
	}
	n := 0
	for _, o := range s.Orders {
		if (o.Status == "0" || o.Status == "1") && (m[TagMassCancelRequestType] != "1" || o.Symbol == sym) {
			n++
		}
	}
	return MassCancelReport(seq, sender, target, now(), m, orDefault(m[TagMassCancelRequestType], "7"), "", n)
}

// parseLax reads tag=value pairs without checking the CheckSum.
func parseLax(raw []byte) map[int]string {
	m := map[int]string{}
	for _, p := range strings.Split(string(raw), soh) {
		if k, v, ok := strings.Cut(p, "="); ok {
			if tag, err := strconv.Atoi(k); err == nil {
				m[tag] = v
			}
		}
	}
	return m
}
