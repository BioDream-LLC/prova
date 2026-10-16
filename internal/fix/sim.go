package fix

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"
)

// Sim is a built-in simulated trading venue. It completes a Logon, answers TestRequests with
// Heartbeats, and turns each NewOrderSingle into an ExecutionReport, deciding fill vs reject against
// a per-account buying-power ledger. Modes let prova be shown catching a defect:
//
//	""/"compliant"  — reject (OrdRejReason=3, "exceeds limit") when notional > buying power, else fill.
//	"approve-all"   — fill every order, including those over buying power (bug).
//	"wrong-reason"  — reject over-limit orders but with OrdRejReason=1 (unknown symbol) instead of 3.
type Sim struct {
	// Buying maps account id -> available buying power in minor units.
	Buying map[string]int64
	Mode   string

	mu   sync.Mutex
	exec int
}

// Decide computes the ExecutionReport outcome for an order: whether to fill, and if not, the
// OrdRejReason and human text.
func (s *Sim) Decide(order map[int]string) (fill bool, rejReason, text string) {
	notional := notionalMinor(order[TagPrice], order[TagOrderQty])
	s.mu.Lock()
	power := s.Buying[order[TagAccount]]
	s.mu.Unlock()
	over := notional > power

	switch s.Mode {
	case "approve-all":
		return true, "", ""
	case "wrong-reason":
		if over {
			return false, "1", "unknown symbol"
		}
		return true, "", ""
	default:
		if over {
			return false, "3", "order notional exceeds buying power"
		}
		return true, "", ""
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
			if _, err := c.Write(Logon(seq, sender, target, now())); err != nil {
				return
			}
			seq++
		case MsgTestRequest:
			if _, err := c.Write(Heartbeat(seq, sender, target, now(), m[TagTestReqID])); err != nil {
				return
			}
			seq++
		case MsgNewOrderSingle:
			fill, rej, text := s.Decide(m)
			var out []byte
			if fill {
				out = ExecFill(seq, sender, target, now(), m, s.nextExec())
			} else {
				out = ExecReject(seq, sender, target, now(), m, s.nextExec(), rej, text)
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
