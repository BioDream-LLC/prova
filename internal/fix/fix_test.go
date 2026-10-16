package fix

import (
	"context"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/adapter"
)

func TestCodecRoundTrip(t *testing.T) {
	msg := EncodeOrder(2, "PROVA", "VENUE", now(), Request{
		ClOrdID: "ORD-9", Account: "ACC1", Symbol: "ACME", Side: "1",
		OrderQty: "15", Price: "10.00", Currency: "USD",
	})
	m, err := parse(msg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m[TagMsgType] != MsgNewOrderSingle {
		t.Fatalf("MsgType=%q", m[TagMsgType])
	}
	if m[TagClOrdID] != "ORD-9" || m[TagOrderQty] != "15" || m[TagPrice] != "10.00" {
		t.Fatalf("fields wrong: %#v", m)
	}
	// Corrupting a byte must fail the checksum.
	bad := append([]byte(nil), msg...)
	bad[10] ^= 0xff
	if _, err := parse(bad); err == nil {
		t.Fatalf("expected checksum failure on corrupted message")
	}
}

func TestNotional(t *testing.T) {
	// 10.00 (1000 cents) x 15 = 15000 cents = $150.00.
	if got := notionalMinor("10.00", "15"); got != 15000 {
		t.Fatalf("notional=%d want 15000", got)
	}
}

func TestSimDecide(t *testing.T) {
	s := &Sim{Buying: map[string]int64{"ACC1": 14200}} // $142.00
	order := map[int]string{TagAccount: "ACC1", TagPrice: "10.00", TagOrderQty: "15", TagSymbol: "ACME", TagSide: "1", TagClOrdID: "O1"}
	fill, reason, _ := s.Decide(order) // 15000 > 14200 -> reject
	if fill || reason != "3" {
		t.Fatalf("expected reject with OrdRejReason 3, got fill=%v reason=%q", fill, reason)
	}
}

func TestEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sim := &Sim{Buying: map[string]int64{"ACC1": 14200}} // $142.00
	addr, err := sim.Serve(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	run := func(qty string) map[string]string {
		conn, err := Adapter{}.Connect(ctx, adapter.Target{Addr: addr}, adapter.SessionConfig{})
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer conn.Close()
		msg, err := conn.Encode(adapter.Template{}, adapter.Record{"Account": "ACC1", "Symbol": "ACME", "Side": "buy", "OrderQty": qty, "Price": "10.00", "ClOrdID": "O-" + qty})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		h, err := conn.Send(ctx, msg)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		dec, err := conn.Await(ctx, h)
		if err != nil {
			t.Fatalf("await: %v", err)
		}
		return dec.Fields
	}

	within := run("9") // $90.00 notional, within $142.00
	if within["decision"] != "accepted" {
		t.Fatalf("within-power order should be accepted: %#v", within)
	}
	over := run("15") // $150.00 notional, over $142.00
	if over["decision"] != "rejected" || over["rejectReason"] != "3" {
		t.Fatalf("over-power order should be rejected with OrdRejReason 3: %#v", over)
	}
}
