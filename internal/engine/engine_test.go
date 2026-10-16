package engine

import (
	"context"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/verdict"
)

// startSim boots an in-process ISO 20022 simulator and returns its address.
func startSim(t *testing.T, ctx context.Context, mode string) string {
	t.Helper()
	s := &iso20022.Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}, Mode: mode}
	addr, err := s.Serve(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestRunEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng := New()
	eng.AddTarget(Target{Name: "ref", Protocol: "iso20022", Addr: startSim(t, ctx, "")})
	eng.AddTarget(Target{Name: "bug", Protocol: "iso20022", Addr: startSim(t, ctx, "accept-all")})

	cases := []struct {
		scenario string
		target   string
		want     verdict.Outcome
	}{
		{"pacs008-over-balance", "ref", verdict.Pass},             // compliant host rejects → correct
		{"pacs008-within-balance", "ref", verdict.Pass},           // compliant host accepts → correct
		{"pacs008-over-balance", "bug", verdict.Fail},             // buggy host accepts over balance → caught
		{"pacs008-unknown-balance", "ref", verdict.Indeterminate}, // balance not seeded → cannot decide
	}
	for _, tc := range cases {
		run, err := eng.Run(ctx, tc.scenario, tc.target)
		if err != nil {
			t.Fatalf("%s@%s: %v", tc.scenario, tc.target, err)
		}
		if run.Outcome != tc.want {
			t.Fatalf("%s@%s: outcome=%s want %s", tc.scenario, tc.target, run.Outcome, tc.want)
		}
		if len(run.Messages) != 2 {
			t.Fatalf("%s@%s: expected request+response messages, got %d", tc.scenario, tc.target, len(run.Messages))
		}
		if len(run.Result.Framework.Args) == 0 {
			t.Fatalf("%s@%s: verdict carried no argumentation framework", tc.scenario, tc.target)
		}
	}

	if got := eng.Runs(); len(got) != len(cases) {
		t.Fatalf("run history = %d, want %d", len(got), len(cases))
	}
	// newest first
	if eng.Runs()[0].ScenarioID != "pacs008-unknown-balance" {
		t.Fatalf("runs not newest-first: %s", eng.Runs()[0].ScenarioID)
	}

	// give the sims a moment to be cancelled cleanly
	cancel()
	time.Sleep(10 * time.Millisecond)
}

func TestRunSuite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := New()
	eng.AddTarget(Target{Name: "ref", Protocol: "iso20022", Addr: startSim(t, ctx, "")})

	res, err := eng.RunSuite(ctx, "regression-iso20022", "ref")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != verdict.Pass {
		t.Fatalf("regression suite against a compliant host should pass, got %s (%+v)", res.Outcome, res)
	}
	if len(res.Runs) != 3 || res.Pass != 3 {
		t.Fatalf("expected 3 passing runs, got %+v", res)
	}
}

func TestRecordReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := New()
	eng.AddTarget(Target{Name: "ref", Protocol: "iso20022", Addr: startSim(t, ctx, "")})

	// Capture a live run, then re-prove it offline — the verdict must reproduce exactly.
	run, err := eng.Run(ctx, "pacs008-over-balance", "ref")
	if err != nil {
		t.Fatal(err)
	}
	rec := RecordingFrom(run)
	if rec.Request["Amount"] != "150.00" || rec.Response["decision"] != "rejected" {
		t.Fatalf("recording did not capture the exchange: %+v", rec)
	}
	replayed := eng.Rejudge(rec)
	if replayed.Outcome != run.Outcome {
		t.Fatalf("replay outcome %s != original %s", replayed.Outcome, run.Outcome)
	}
	if replayed.Result.Certificate == nil {
		t.Fatal("replayed run should still carry a certificate")
	}

	// Round-trip through the JSONL cassette.
	path := t.TempDir() + "/cassette.jsonl"
	if err := WriteRecordings(path, []Recording{rec}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRecordings(path)
	if err != nil || len(got) != 1 || got[0].ScenarioID != "pacs008-over-balance" {
		t.Fatalf("cassette round-trip failed: %v %+v", err, got)
	}
	res := eng.Replay(got)
	if res.Outcome != verdict.Pass || res.Pass != 1 {
		t.Fatalf("replay of a compliant-host rejection should pass: %+v", res)
	}
}

func TestRunLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := New()
	eng.AddTarget(Target{Name: "ref", Protocol: "iso20022", Addr: startSim(t, ctx, "")})

	lr, err := eng.RunLoad(ctx, "pacs008-within-balance", "ref", 50, 8)
	if err != nil {
		t.Fatal(err)
	}
	if lr.N != 50 || lr.Errors != 0 {
		t.Fatalf("load run wrong: %+v", lr)
	}
	if lr.Outcomes["pass"] != 50 {
		t.Fatalf("all 50 should pass, got %v", lr.Outcomes)
	}
	if lr.Throughput <= 0 {
		t.Fatalf("throughput should be positive: %+v", lr)
	}
	// Load runs must not pollute the stored history.
	if len(eng.Runs()) != 0 {
		t.Fatalf("load runs should not be stored, found %d", len(eng.Runs()))
	}
}
