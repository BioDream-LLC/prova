package capture

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/fix"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/iso8583"
	"github.com/biodream-llc/prova/internal/sims"
	"github.com/biodream-llc/prova/internal/verdict"
)

// fixLog is a QuickFIX-style log: one message per line with a timestamp prefix, SOH shown as |.
func fixLog() string {
	var b strings.Builder
	t := "20261030-09:00:00.000"
	msgs := [][]byte{
		fix.Logon(1, "PROVA", "VENUE", t),
		fix.EncodeOrder(2, "PROVA", "VENUE", t, fix.FromRecord(map[string]string{"ClOrdID": "C-1", "Symbol": "ACME", "OrderQty": "9", "Price": "10.00"})),
		fix.EncodeOrder(3, "PROVA", "VENUE", "20261030-09:00:01.000", fix.FromRecord(map[string]string{"ClOrdID": "C-2", "Symbol": "ACME", "OrderQty": "15", "Price": "10.00"})),
		fix.Heartbeat(4, "PROVA", "VENUE", t, ""),
	}
	for _, m := range msgs {
		b.WriteString(t + " : " + strings.ReplaceAll(string(m), "\x01", "|") + "\n")
	}
	return b.String()
}

func TestImportFIX(t *testing.T) {
	recs, rep, err := Import("fix", strings.NewReader(fixLog()), Options{Context: map[string]string{"availableBalance": "142.00"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Read != 4 || rep.Kept != 2 || rep.Skipped != 2 || len(recs) != 2 {
		t.Fatalf("report %+v", rep)
	}
	r := recs[0]
	if r.Message != "D" || r.Request["ClOrdID"] != "C-1" || r.Request["Side"] != "buy" || r.Request["Amount"] != "90.00" || r.Context["availableBalance"] != "142.00" {
		t.Errorf("recording %+v", r)
	}
	if !recs[1].At.After(recs[0].At) {
		t.Errorf("timestamps not read: %v %v", recs[0].At, recs[1].At)
	}
}

func TestImportISO8583Dialects(t *testing.T) {
	for _, d := range iso8583.Dialects {
		var lines []string
		for _, req := range []iso8583.Request{
			{MTI: "0100", PAN: "4111111111111111", ProcessingCode: "000000", Amount: "90.00", Currency: "EUR", STAN: "000101", Terminal: "TERM0001", Expiry: "3012"},
			{MTI: "0400", PAN: "4111111111111111", ProcessingCode: "000000", Amount: "90.00", Currency: "EUR", STAN: "000102", Terminal: "TERM0001", OriginalSTAN: "000201"},
		} {
			raw, err := iso8583.EncodeIn(d, req)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, hex.EncodeToString(raw))
		}
		resp, _ := iso8583.EncodeResponseIn(d, "0100", map[int]string{11: "000101"}, "00")
		lines = append(lines, hex.EncodeToString(resp))
		recs, rep, err := Import("iso8583", strings.NewReader(strings.Join(lines, "\n")), Options{Dialect: d.Name})
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		if rep.Kept != 2 || rep.Skipped != 1 {
			t.Errorf("%s: %+v", d.Name, rep)
		}
		if recs[0].Request["Amount"] != "90.00" || recs[0].Request["Currency"] != "EUR" || recs[1].Request["OriginalSTAN"] != "000201" || recs[1].Message != "0400" {
			t.Errorf("%s: %+v / %+v", d.Name, recs[0].Request, recs[1])
		}
	}
}

func TestImportISO20022AndMask(t *testing.T) {
	var docs []string
	for _, rec := range []map[string]string{
		{"Message": "pacs.008", "EndToEndId": "E-1", "Amount": "90.00", "DebtorIBAN": "DE89370400440532013000", "DebtorAcct": "DE89370400440532013000", "CreditIBAN": "FR1420041010050500013M02606", "DebtorName": "Real Person"},
		{"Message": "camt.056", "EndToEndId": "X-1", "OrgnlEndToEndId": "E-1", "Reason": "DUPL"},
	} {
		raw, err := iso20022.Encode(iso20022.FromRecord(rec))
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, string(raw))
	}
	st, _ := iso20022.EncodeStatus(iso20022.Status{TxSts: "ACSC", OrgnlEndToEndId: "E-1"})
	docs = append(docs, string(st))
	recs, rep, err := Import("iso20022", strings.NewReader(strings.Join(docs, "\n")), Options{Mask: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kept != 2 || rep.Skipped != 1 || recs[0].Message != "pacs.008" || recs[1].Message != "camt.056" {
		t.Fatalf("%+v", rep)
	}
	m := recs[0].Request
	if m["DebtorIBAN"] == "DE89370400440532013000" || m["DebtorName"] == "Real Person" || !strings.HasPrefix(m["DebtorIBAN"], "DE") {
		t.Errorf("not masked: %v", m)
	}
	if len(verdict.FieldProblems(map[string]string{"DebtorIBAN": m["DebtorIBAN"], "CreditIBAN": m["CreditIBAN"]})) != 0 {
		t.Errorf("masked IBANs must stay valid: %v", m)
	}
	if maskPAN("4111111111111111") == "4111111111111111" || len(verdict.FieldProblems(map[string]string{"PAN": maskPAN("4111111111111111")})) != 0 {
		t.Error("masked PAN is not a different valid card number")
	}
	if maskPAN("4111111111111111") != maskPAN("4111111111111111") {
		t.Error("masking is not stable")
	}
}

// TestReplayImportedTrafficLive imports a FIX log and replays it at the simulated venue over one
// session per worker, judging every order.
func TestReplayImportedTrafficLive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := engine.New()
	targets, err := sims.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		eng.AddTarget(engine.Target{Name: tg.Name, Protocol: tg.Protocol, Addr: tg.Addr, Params: tg.Params})
	}
	recs, _, err := Import("fix", strings.NewReader(fixLog()), Options{Context: map[string]string{"availableBalance": "142.00"}})
	if err != nil {
		t.Fatal(err)
	}
	lr, err := eng.RunLoadSpec(ctx, engine.LoadSpec{Replay: recs, Target: "fix-venue", N: 40, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Half the orders are within buying power (filled: pass), half over it (rejected: pass).
	if lr.N != 40 || lr.Errors != 0 || lr.Outcomes["pass"] != 40 {
		t.Errorf("replay: %+v", lr)
	}
	bug, err := eng.RunLoadSpec(ctx, engine.LoadSpec{Replay: recs, Target: "fix-venue-bug", N: 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if bug.Outcomes["fail"] != 10 {
		t.Errorf("the defective venue's over-limit fills should fail: %+v", bug)
	}
}
