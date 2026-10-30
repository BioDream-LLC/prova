package engine

import (
	"context"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/sims"
)

func startAll(t *testing.T, ctx context.Context) *Engine {
	t.Helper()
	eng := New()
	targets, err := sims.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		eng.AddTarget(Target{Name: tg.Name, Protocol: tg.Protocol, Addr: tg.Addr, Params: tg.Params})
	}
	return eng
}

// TestLoadPacing: a rate spaces the requests evenly; a duration stops the run on time; every protocol
// keeps one connection per worker; the timeline has a row per second.
func TestLoadPacing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := startAll(t, ctx)

	start := time.Now()
	lr, err := eng.RunLoadSpec(ctx, LoadSpec{Scenario: "pacs008-within-balance", Target: "cbpr-uat", N: 30, Concurrency: 4, Rate: 60})
	if err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 450*time.Millisecond {
		t.Errorf("30 requests at 60/s took %s, want about 0.5 s", el)
	}
	if lr.N != 30 || lr.Errors != 0 || lr.Outcomes["pass"] != 30 || len(lr.Timeline) == 0 {
		t.Errorf("paced run: %+v", lr)
	}

	start = time.Now()
	lr, err = eng.RunLoadSpec(ctx, LoadSpec{Scenario: "fix-within-limit", Target: "fix-venue", Duration: 400 * time.Millisecond, Concurrency: 3})
	if err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("a 400 ms run took %s", el)
	}
	if lr.N < 50 || lr.Errors != 0 || lr.Outcomes["fail"] != 0 {
		t.Errorf("FIX duration run over persistent sessions: %+v", lr)
	}

	for _, sc := range []struct{ scenario, target string }{{"visa-chip-valid", "visa-switch"}, {"iso8583-within-limit", "mc-switch"}} {
		lr, err = eng.RunLoadSpec(ctx, LoadSpec{Scenario: sc.scenario, Target: sc.target, N: 50, Concurrency: 5})
		if err != nil || lr.Errors != 0 || lr.Outcomes["pass"] != 50 {
			t.Errorf("%s @ %s: %+v %v", sc.scenario, sc.target, lr, err)
		}
	}
	if _, err := eng.RunLoadSpec(ctx, LoadSpec{Scenario: "pacs008-within-balance", Target: "fix-venue", N: 1}); err != nil {
		t.Fatal(err) // a protocol mismatch is a per-request error, counted, not a crash
	}
	if _, err := eng.RunLoadSpec(ctx, LoadSpec{Scenario: "pacs008-within-balance", Target: "cbpr-uat", N: 1, Concurrency: 10000}); err == nil {
		t.Error("an absurd concurrency was accepted")
	}
}
