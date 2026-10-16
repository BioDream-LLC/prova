package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/biodream-llc/prova/internal/verdict"
)

// Suite is a named set of scenarios run together — the regression / certification pack competitors
// ship. Target is the default target; a run may override it.
type Suite struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Scenarios   []string `json:"scenarios"`
	Target      string   `json:"target"`
}

// SuiteResult aggregates a suite execution.
type SuiteResult struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Target        string          `json:"target"`
	At            time.Time       `json:"at"`
	Outcome       verdict.Outcome `json:"outcome"`
	Pass          int             `json:"pass"`
	Fail          int             `json:"fail"`
	Indeterminate int             `json:"indeterminate"`
	DurationMs    int64           `json:"durationMs"`
	Runs          []Run           `json:"runs"`
	RunID         string          `json:"runId,omitempty"` // this execution
	By            string          `json:"by,omitempty"`
}

// LoadResult aggregates a load run: latency percentiles, throughput and the outcome spread.
type LoadResult struct {
	Scenario    string         `json:"scenario"`
	Target      string         `json:"target"`
	N           int            `json:"n"`
	Concurrency int            `json:"concurrency"`
	DurationMs  int64          `json:"durationMs"`
	Throughput  float64        `json:"throughput"` // requests per second
	P50         float64        `json:"p50"`
	P95         float64        `json:"p95"`
	P99         float64        `json:"p99"`
	Max         float64        `json:"max"`
	Errors      int            `json:"errors"`
	Outcomes    map[string]int `json:"outcomes"`
	At          time.Time      `json:"at"`
	By          string         `json:"by,omitempty"`
}

// Suites lists the shipped suites.
func (e *Engine) Suites() []Suite {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Suite(nil), e.suites...)
}

func (e *Engine) suite(id string) (Suite, bool) {
	for _, s := range e.suites {
		if s.ID == id {
			return s, true
		}
	}
	return Suite{}, false
}

// RunSuite runs every scenario in a suite against its target (or targetOverride), stores each run
// and returns the aggregate. The aggregate outcome is Fail if any scenario failed, else
// Indeterminate if any was indeterminate, else Pass.
func (e *Engine) RunSuite(ctx context.Context, suiteID, targetOverride string) (SuiteResult, error) {
	e.mu.Lock()
	s, ok := e.suite(suiteID)
	e.mu.Unlock()
	if !ok {
		return SuiteResult{}, fmt.Errorf("engine: unknown suite %q", suiteID)
	}
	target := s.Target
	if targetOverride != "" {
		target = targetOverride
	}
	res := SuiteResult{ID: s.ID, Name: s.Name, Target: target, At: time.Now().UTC(), Outcome: verdict.Pass,
		RunID: fmt.Sprintf("s-%d", time.Now().UnixNano()), By: actorFrom(ctx)}
	start := time.Now()
	for _, scID := range s.Scenarios {
		run, err := e.Run(ctx, scID, target)
		if err != nil {
			return SuiteResult{}, err
		}
		res.Runs = append(res.Runs, run)
		switch run.Outcome {
		case verdict.Pass:
			res.Pass++
		case verdict.Fail:
			res.Fail++
		case verdict.Indeterminate:
			res.Indeterminate++
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	if res.Fail > 0 {
		res.Outcome = verdict.Fail
	} else if res.Indeterminate > 0 {
		res.Outcome = verdict.Indeterminate
	}
	e.mu.Lock()
	st := e.st
	e.mu.Unlock()
	if st != nil {
		ids := make([]string, len(res.Runs))
		for i := range res.Runs {
			res.Runs[i].SuiteRun = res.RunID
			ids[i] = res.Runs[i].ID
		}
		if err := st.SaveSuite(res, ids); err != nil {
			return SuiteResult{}, fmt.Errorf("engine: saving suite run: %w", err)
		}
		e.mu.Lock()
		for i := range e.runs {
			for _, id := range ids {
				if e.runs[i].ID == id {
					e.runs[i].SuiteRun = res.RunID
				}
			}
		}
		e.mu.Unlock()
	}
	return res, nil
}

// RunLoad executes scenarioID against target n times across concurrency workers and reports latency
// percentiles and throughput. The individual requests are not stored (a load run can be large); the summary is.
func (e *Engine) RunLoad(ctx context.Context, scenarioID, target string, n, concurrency int) (LoadResult, error) {
	e.mu.Lock()
	sc, ok := e.scenario(scenarioID)
	e.mu.Unlock()
	if !ok {
		return LoadResult{}, fmt.Errorf("engine: unknown scenario %q", scenarioID)
	}
	if n <= 0 {
		n = 1
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > n {
		concurrency = n
	}

	type one struct {
		lat float64
		out verdict.Outcome
		err error
	}
	results := make([]one, n)
	tasks := make(chan int)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range tasks {
				run, err := e.execOnce(ctx, sc, target)
				if err != nil {
					results[i] = one{err: err}
				} else {
					results[i] = one{lat: run.LatencyMs, out: run.Outcome}
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		tasks <- i
	}
	close(tasks)
	wg.Wait()
	dur := time.Since(start)

	lr := LoadResult{
		Scenario: sc.Name, Target: target, N: n, Concurrency: concurrency,
		DurationMs: dur.Milliseconds(), At: time.Now().UTC(),
		Outcomes: map[string]int{},
	}
	if secs := dur.Seconds(); secs > 0 {
		lr.Throughput = float64(n) / secs
	}
	var lats []float64
	for _, r := range results {
		if r.err != nil {
			lr.Errors++
			continue
		}
		lats = append(lats, r.lat)
		lr.Outcomes[string(r.out)]++
	}
	lr.P50 = percentile(lats, 50)
	lr.P95 = percentile(lats, 95)
	lr.P99 = percentile(lats, 99)
	lr.Max = percentile(lats, 100)
	lr.By = actorFrom(ctx)
	e.mu.Lock()
	st := e.st
	e.mu.Unlock()
	if st != nil {
		if err := st.SaveLoad(lr); err != nil {
			return LoadResult{}, fmt.Errorf("engine: saving load run: %w", err)
		}
	}
	return lr, nil
}

func percentile(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := (p * len(s)) / 100
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

// builtinSuites are the shipped regression and certification packs.
func builtinSuites() []Suite {
	return []Suite{
		{
			ID: "regression-iso20022", Name: "ISO 20022 regression", Target: "cbpr-uat",
			Description: "Core pacs.008 behaviours: over-balance, within-balance and the exact-balance boundary.",
			Scenarios:   []string{"pacs008-over-balance", "pacs008-within-balance", "pacs008-exact-balance"},
		},
		{
			ID: "regression-iso8583", Name: "ISO 8583 regression", Target: "card-switch",
			Description: "Core 0100 authorisation behaviours: over-limit decline and within-limit approval.",
			Scenarios:   []string{"iso8583-over-limit", "iso8583-within-limit"},
		},
		{
			ID: "regression-fix", Name: "FIX regression", Target: "fix-venue",
			Description: "Core NewOrderSingle behaviours: order over buying power rejected, order within buying power filled.",
			Scenarios:   []string{"fix-over-limit", "fix-within-limit"},
		},
		{
			ID: "certification-cbpr", Name: "CBPR+ certification pack", Target: "cbpr-uat",
			Description: "A certification-style pass over the pacs.008 cases, including the unseeded-balance indeterminate.",
			Scenarios:   []string{"pacs008-over-balance", "pacs008-within-balance", "pacs008-exact-balance", "pacs008-unknown-balance"},
		},
		{
			ID: "controls-iso20022", Name: "ISO 20022 controls", Target: "cbpr-uat",
			Description: "Payment-control behaviours beyond funds: wrong-currency (AM03), duplicate (AM05) and back-valued (DT01) instructions must all be rejected.",
			Scenarios:   []string{"pacs008-currency-mismatch", "pacs008-duplicate", "pacs008-back-valued"},
		},
	}
}
