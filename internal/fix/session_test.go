package fix

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"
)

// TestSessionRecoveryAndFIX50 exercises the session layer beyond the order flow: a FIXT.1.1 / FIX 5.0
// SP2 logon (DefaultApplVerID carried and echoed) and a ResendRequest answered by a SequenceReset in
// gap-fill mode. It drives the sim over a raw socket so the session administration is tested directly.
func TestSessionRecoveryAndFIX50(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sim := &Sim{Buying: map[string]int64{"ACC1": 100000}, Listed: map[string]bool{"ACME": true}}
	addr, err := sim.Serve(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nc, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(nc)

	read := func() map[int]string {
		raw, err := readMessage(r)
		if err != nil {
			t.Fatal(err)
		}
		m, err := parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	// 1. FIXT.1.1 logon for FIX 5.0 SP2: the venue must reply over FIXT.1.1 and echo the app version.
	if _, err := nc.Write(LogonV(BeginStringT11, ApplVer50SP2, 1, "PROVA", "VENUE", now())); err != nil {
		t.Fatal(err)
	}
	logon := read()
	if logon[TagMsgType] != MsgLogon {
		t.Fatalf("expected Logon, got %q", logon[TagMsgType])
	}
	if logon[TagBeginString] != BeginStringT11 {
		t.Errorf("BeginString = %q, want %q", logon[TagBeginString], BeginStringT11)
	}
	if logon[TagDefaultApplVerID] != ApplVer50SP2 {
		t.Errorf("DefaultApplVerID = %q, want %q (FIX 5.0 SP2)", logon[TagDefaultApplVerID], ApplVer50SP2)
	}

	// 2. ResendRequest -> SequenceReset gap-fill.
	if _, err := nc.Write(ResendRequest(BeginStringT11, 2, "PROVA", "VENUE", now(), 2, 4)); err != nil {
		t.Fatal(err)
	}
	reset := read()
	if reset[TagMsgType] != MsgSequenceReset {
		t.Fatalf("expected SequenceReset, got %q", reset[TagMsgType])
	}
	if reset[TagGapFillFlag] != "Y" {
		t.Errorf("GapFillFlag = %q, want Y", reset[TagGapFillFlag])
	}
	if reset[TagNewSeqNo] != "5" {
		t.Errorf("NewSeqNo = %q, want 5 (past the requested end)", reset[TagNewSeqNo])
	}

	// 3. The session still carries orders afterwards.
	order := EncodeOrder(3, "PROVA", "VENUE", now(), Request{ClOrdID: "ORD-1", Account: "ACC1", Symbol: "ACME", Side: "1", OrderQty: "5", Price: "10.00"})
	if _, err := nc.Write(order); err != nil {
		t.Fatal(err)
	}
	exec := read()
	if exec[TagMsgType] != MsgExecutionReport || exec[TagOrdStatus] != "2" {
		t.Errorf("expected a fill, got MsgType=%q OrdStatus=%q", exec[TagMsgType], exec[TagOrdStatus])
	}
}

// TestPartialFill checks the venue reports a partial fill (ExecType=1, OrdStatus=1) with a cumulative
// quantity below the order and the remainder left working.
func TestPartialFill(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sim := &Sim{Buying: map[string]int64{"ACC1": 100000}, Listed: map[string]bool{"PARTL": true}, Partial: map[string]bool{"PARTL": true}}
	addr, err := sim.Serve(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nc, _ := net.DialTimeout("tcp", addr, 2*time.Second)
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(nc)
	_, _ = nc.Write(Logon(1, "PROVA", "VENUE", now()))
	if _, err := readMessage(r); err != nil {
		t.Fatal(err)
	}
	_, _ = nc.Write(EncodeOrder(2, "PROVA", "VENUE", now(), Request{ClOrdID: "ORD-P", Account: "ACC1", Symbol: "PARTL", Side: "1", OrderQty: "8", Price: "10.00"}))
	raw, err := readMessage(r)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := DecodeResponse(raw)
	f := st.Fields()
	if f["decision"] != "accepted" || st.ExecType != "1" || st.OrdStatus != "1" {
		t.Fatalf("expected a partial fill, got %+v", st)
	}
	if st.CumQty != "4" || st.LeavesQty != "4" {
		t.Errorf("CumQty/LeavesQty = %s/%s, want 4/4", st.CumQty, st.LeavesQty)
	}
}
