package main

import (
	"context"
	"flag"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/store"
)

// TestTheManualBuildsFromTheServer: the manual the server serves includes the reference generated from its own
// scenarios, suites, targets and options, and every cross-reference in it resolves.
func TestTheManualBuildsFromTheServer(t *testing.T) {
	fs := flag.NewFlagSet("prova", flag.ContinueOnError)
	defineServerFlags(fs)
	eng := engine.New()
	for _, s := range simTargets {
		eng.AddTarget(engine.Target{Name: s.name, Protocol: s.protocol, Note: s.note})
	}
	doc, err := buildManual(eng, fs)
	if err != nil {
		t.Fatal(err)
	}
	html := doc.HTML()
	for _, want := range []string{"Scenario Reference", "pacs008-over-balance", "regression-fix", "fix-venue-bug",
		"-demo-account", "/api/reports/runs.csv"} {
		if !strings.Contains(html, want) {
			t.Errorf("the manual does not mention %q", want)
		}
	}
	anchors := doc.Anchors()
	for _, m := range regexp.MustCompile(`href="#([^"]+)"`).FindAllStringSubmatch(html, -1) {
		if _, ok := anchors[m[1]]; !ok {
			t.Errorf("dangling link #%s", m[1])
		}
	}
}

// TestRunsPersistAndReport: runs made by an engine with a store survive a new engine over the same database (a
// restart), keep who made them and their suite, and are counted by the report.
func TestRunsPersistAndReport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prova.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng := engine.New()
	if err := eng.Attach(runStore{st}); err != nil {
		t.Fatal(err)
	}
	startSims(ctx, eng, false)
	res, err := eng.RunSuite(engine.WithActor(ctx, "alice"), "regression-iso20022", "switch-sim")
	if err != nil {
		t.Fatal(err)
	}
	if res.Fail == 0 {
		t.Fatalf("the accept-all host should fail the regression suite: %+v", res)
	}

	again := engine.New()
	if err := again.Attach(runStore{st}); err != nil {
		t.Fatal(err)
	}
	runs := again.Runs()
	if len(runs) != len(res.Runs) {
		t.Fatalf("after a restart %d runs, want %d", len(runs), len(res.Runs))
	}
	got, ok := again.Get(res.Runs[0].ID)
	if !ok || got.By != "alice" || got.SuiteRun != res.RunID || len(got.Result.Chain) == 0 {
		t.Fatalf("stored run lost detail: ok=%v by=%q suite=%q chain=%d", ok, got.By, got.SuiteRun, len(got.Result.Chain))
	}
	// Numbering continues rather than reusing ids.
	startSims(ctx, again, false)
	r, err := again.Run(ctx, "pacs008-over-balance", "cbpr-uat")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range res.Runs {
		if old.ID == r.ID {
			t.Fatalf("run id %s reused after a restart", r.ID)
		}
	}

	rep, err := buildReport(st, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != len(res.Runs)+1 || rep.Totals.Fail != res.Fail || len(rep.Suites) != 1 || rep.Suites[0].Actor != "alice" {
		t.Fatalf("report: %+v", rep)
	}
	if len(rep.ByDay) != 7 || rep.ByActor[0].Key != "alice" {
		t.Fatalf("report breakdowns: days=%d actors=%+v", len(rep.ByDay), rep.ByActor)
	}
}

func TestCSVSafe(t *testing.T) {
	if csvSafe("=cmd()") != "'=cmd()" || csvSafe("ok") != "ok" {
		t.Fatal("csvSafe")
	}
}
