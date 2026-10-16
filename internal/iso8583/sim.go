package iso8583

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Sim is a built-in simulated authorisation switch. It accepts 0100 requests and answers 0110,
// deciding the DE39 response code against a per-PAN ledger. Modes let prova be shown catching a
// defect:
//
//	""/"compliant"   — DE39=51 (insufficient funds) when the amount exceeds the balance, else 00.
//	"approve-all"    — DE39=00 always (bug: approves over the limit).
//	"wrong-code"     — rejects over-limit but with DE39=05 (do not honour) instead of 51.
type Sim struct {
	// Ledger maps PAN -> available balance in minor units.
	Ledger map[string]int64
	Mode   string
	mu     sync.Mutex
}

// Decide computes the DE39 response code for a request.
func (s *Sim) Decide(req map[int]string) string {
	pan := req[2]
	amt, _ := strconv.ParseInt(strings.TrimLeft(req[4], "0"), 10, 64)
	s.mu.Lock()
	avail := s.Ledger[pan]
	s.mu.Unlock()
	short := amt > avail

	switch s.Mode {
	case "approve-all":
		return "00"
	case "wrong-code":
		if short {
			return "05"
		}
		return "00"
	default:
		if short {
			return "51"
		}
		return "00"
	}
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
		_, fields, err := Unpack(raw)
		if err != nil {
			return
		}
		out, err := EncodeResponse(fields, s.Decide(fields))
		if err != nil {
			return
		}
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}
