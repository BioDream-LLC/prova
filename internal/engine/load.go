package engine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/biodream-llc/prova/internal/adapter"
	"github.com/biodream-llc/prova/internal/verdict"
)

// LoadSpec describes a load run: what to send, at whom, how much and how fast. Exactly one of
// Scenario and Replay names the traffic.
type LoadSpec struct {
	Scenario    string        `json:"scenario,omitempty"`
	Replay      []Recording   `json:"-"` // recorded or imported traffic, replayed live in order
	Target      string        `json:"target"`
	N           int           `json:"n"`           // requests; for a replay, 0 means every recording once
	Duration    time.Duration `json:"duration"`    // keep sending until this has passed (overrides N when set)
	Concurrency int           `json:"concurrency"` // workers, each with its own connection
	Rate        float64       `json:"rate"`        // requests per second across all workers; 0 is as fast as possible
	Speed       float64       `json:"speed"`       // a replay's pace against its recorded timing: 1 is real time, 0 ignores the timing
}

// loadLimits keep one load run inside what a test harness should send.
const (
	maxLoadRequests    = 1_000_000
	maxLoadConcurrency = 512
	maxLoadDuration    = time.Hour
)

// Second is one second of a load run, for the timeline.
type Second struct {
	At     int     `json:"at"` // seconds since the start
	Sent   int     `json:"sent"`
	Errors int     `json:"errors"`
	P95    float64 `json:"p95"`
}

// task is one request a worker sends.
type task struct {
	i       int
	sc      Scenario
	due     time.Time // when to send it (zero: at once)
	protoID string
}

// RunLoad executes scenarioID against target n times across concurrency workers and reports latency
// percentiles and throughput. It is RunLoadSpec without a rate, duration or replay.
func (e *Engine) RunLoad(ctx context.Context, scenarioID, target string, n, concurrency int) (LoadResult, error) {
	return e.RunLoadSpec(ctx, LoadSpec{Scenario: scenarioID, Target: target, N: n, Concurrency: concurrency})
}

// RunLoadSpec runs a load test. Every worker holds one connection (one FIX session) for the whole run
// and reconnects only after an error, so the target sees the steady traffic a real counterparty sends.
// Every response is judged by the casebook, exactly as a single run is; the summary counts the
// outcomes. A replay sends each recording's request with its own message type and context; with a
// speed it keeps the recorded spacing (scaled), with a rate it paces evenly.
func (e *Engine) RunLoadSpec(ctx context.Context, spec LoadSpec) (LoadResult, error) {
	var base Scenario
	if len(spec.Replay) == 0 {
		e.mu.Lock()
		sc, ok := e.scenario(spec.Scenario)
		e.mu.Unlock()
		if !ok {
			return LoadResult{}, fmt.Errorf("engine: unknown scenario %q", spec.Scenario)
		}
		base = sc
	}
	if spec.Concurrency <= 0 {
		spec.Concurrency = 1
	}
	if spec.Concurrency > maxLoadConcurrency {
		return LoadResult{}, fmt.Errorf("engine: at most %d workers", maxLoadConcurrency)
	}
	if spec.Duration > maxLoadDuration {
		return LoadResult{}, fmt.Errorf("engine: a load run lasts at most %s", maxLoadDuration)
	}
	if spec.N <= 0 {
		spec.N = len(spec.Replay)
		if spec.N == 0 {
			spec.N = 1
		}
	}
	if spec.Duration > 0 {
		spec.N = maxLoadRequests
	}
	if spec.N > maxLoadRequests {
		return LoadResult{}, fmt.Errorf("engine: at most %d requests in one load run", maxLoadRequests)
	}
	if spec.Concurrency > spec.N {
		spec.Concurrency = spec.N
	}

	// The traffic, as scenarios: the one scenario repeated, or the replayed recordings in order.
	scenarioAt := func(i int) Scenario {
		if len(spec.Replay) == 0 {
			return base
		}
		r := spec.Replay[i%len(spec.Replay)]
		return Scenario{ID: r.ScenarioID, Name: orDefaultStr(r.Scenario, r.ScenarioID), Protocol: r.Protocol, Message: r.Message,
			Request: r.Request, Context: r.Context}
	}
	var recStart time.Time
	if len(spec.Replay) > 0 {
		recStart = spec.Replay[0].At
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := time.Now()
	// feed stops at the run's duration: no request starts after it, but one in flight finishes and counts.
	feed := ctx
	if spec.Duration > 0 {
		var stop context.CancelFunc
		feed, stop = context.WithDeadline(ctx, start.Add(spec.Duration))
		defer stop()
	}

	type one struct {
		lat float64
		out verdict.Outcome
		err error
		at  time.Duration
	}
	var (
		mu      sync.Mutex
		results []one
	)
	tasks := make(chan task, spec.Concurrency)
	var wg sync.WaitGroup
	for w := 0; w < spec.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conns := map[string]*loadConn{} // one connection per protocol this worker sends
			defer func() {
				for _, c := range conns {
					c.close()
				}
			}()
			for t := range tasks {
				if d := time.Until(t.due); d > 0 {
					select {
					case <-time.After(d):
					case <-feed.Done():
						continue
					}
				}
				if feed.Err() != nil {
					continue // the run is over
				}
				run, err := e.sendOn(ctx, conns, t.sc, spec.Target)
				r := one{err: err, at: time.Since(start)}
				if err == nil {
					r.lat, r.out = run.LatencyMs, run.Outcome
				}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}

	var interval time.Duration
	if spec.Rate > 0 {
		interval = time.Duration(float64(time.Second) / spec.Rate)
	}
sending:
	for i := 0; i < spec.N; i++ {
		sc := scenarioAt(i)
		var due time.Time
		switch {
		case len(spec.Replay) > 0 && spec.Speed > 0 && spec.Rate == 0:
			off := spec.Replay[i%len(spec.Replay)].At.Sub(recStart)
			if off < 0 {
				off = 0
			}
			due = start.Add(time.Duration(float64(off) / spec.Speed))
		case interval > 0:
			due = start.Add(time.Duration(i) * interval)
		}
		select {
		case tasks <- task{i: i, sc: sc, due: due}:
		case <-feed.Done():
			break sending
		}
	}
	close(tasks)
	wg.Wait()
	dur := time.Since(start)

	name := base.Name
	if len(spec.Replay) > 0 {
		name = fmt.Sprintf("replay of %d recordings", len(spec.Replay))
	}
	lr := LoadResult{
		Scenario: name, Target: spec.Target, N: len(results), Concurrency: spec.Concurrency,
		DurationMs: dur.Milliseconds(), At: time.Now().UTC(), Outcomes: map[string]int{}, Rate: spec.Rate,
	}
	if secs := dur.Seconds(); secs > 0 {
		lr.Throughput = float64(len(results)) / secs
	}
	var lats []float64
	perSec := map[int][]float64{}
	errSec := map[int]int{}
	for _, r := range results {
		s := int(r.at / time.Second)
		if r.err != nil {
			lr.Errors++
			errSec[s]++
			if lr.FirstError == "" {
				lr.FirstError = r.err.Error()
			}
			continue
		}
		lats = append(lats, r.lat)
		perSec[s] = append(perSec[s], r.lat)
		lr.Outcomes[string(r.out)]++
	}
	lr.P50 = percentile(lats, 50)
	lr.P95 = percentile(lats, 95)
	lr.P99 = percentile(lats, 99)
	lr.Max = percentile(lats, 100)
	lr.Mean = mean(lats)
	secs := int(math.Ceil(dur.Seconds()))
	for s := 0; s < secs && s < 3600; s++ {
		lr.Timeline = append(lr.Timeline, Second{At: s, Sent: len(perSec[s]) + errSec[s], Errors: errSec[s], P95: percentile(perSec[s], 95)})
	}
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

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// loadConn is a worker's open connection to the target for one protocol.
type loadConn struct {
	c adapter.Conn
}

func (l *loadConn) close() {
	if l != nil && l.c != nil {
		_ = l.c.Close()
	}
}

// sendOn sends one scenario's request over the worker's open connection for its protocol (opening it
// the first time, and again after an error), judges the response and returns the run, unstored.
func (e *Engine) sendOn(ctx context.Context, conns map[string]*loadConn, sc Scenario, targetName string) (Run, error) {
	e.mu.Lock()
	tgt, ok := e.targets[targetName]
	book := e.book
	e.mu.Unlock()
	if !ok {
		return Run{}, fmt.Errorf("engine: unknown target %q", targetName)
	}
	if sc.Protocol != tgt.Protocol {
		return Run{}, fmt.Errorf("engine: a %s message cannot go to the %s target %s", sc.Protocol, tgt.Protocol, targetName)
	}
	a, ok := adapter.Get(sc.Protocol)
	if !ok {
		return Run{}, fmt.Errorf("engine: no adapter for protocol %q", sc.Protocol)
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lc := conns[sc.Protocol]
	if lc == nil {
		c, err := a.Connect(rctx, adapter.Target{Addr: tgt.Addr, Params: tgt.Params}, adapter.SessionConfig{Params: tgt.Params})
		if err != nil {
			return Run{}, err
		}
		lc = &loadConn{c: c}
		conns[sc.Protocol] = lc
	}
	fail := func(err error) (Run, error) {
		lc.close()
		delete(conns, sc.Protocol) // reconnect for the next request
		return Run{}, err
	}
	msg, err := lc.c.Encode(adapter.Template{Name: sc.MessageKind()}, sc.Request)
	if err != nil {
		return Run{}, err // the message, not the connection, is at fault
	}
	sc.Request = schemaEvidence(sc, msg)
	h, err := lc.c.Send(rctx, msg)
	if err != nil {
		return fail(err)
	}
	dec, err := lc.c.Await(rctx, h)
	if err != nil {
		return fail(err)
	}
	lat, _ := strconv.ParseFloat(dec.Meta["latencyMs"], 64)
	res := judge(book, verdict.Case{Request: sc.Request, Context: sc.Context, Response: dec.Fields})
	return Run{ScenarioID: sc.ID, Protocol: sc.Protocol, Target: targetName, LatencyMs: lat, Outcome: res.Outcome, Result: res}, nil
}

func percentile(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	idx := (p * len(s)) / 100
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}
