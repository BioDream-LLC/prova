package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/biodream-llc/prova/internal/proof"
	"github.com/biodream-llc/prova/internal/verdict"
)

// Recording is a captured message exchange: the request that was sent, the state the tester had
// seeded, and the exact normalized response the target returned. It is the unit of record & replay —
// capture real (or simulated) traffic once, then re-prove it offline against the casebook as a
// regression, with no live target. It is protocol-agnostic because it stores only normalized fields.
type Recording struct {
	ScenarioID string            `json:"scenarioId"`
	Scenario   string            `json:"scenario"`
	Protocol   string            `json:"protocol"`
	Target     string            `json:"target"`
	Request    map[string]string `json:"request"`
	Context    map[string]string `json:"context,omitempty"`
	Response   map[string]string `json:"response"`
	Outcome    verdict.Outcome   `json:"outcome"`
	At         time.Time         `json:"at"`
}

// RecordingFrom captures a completed run as a Recording.
func RecordingFrom(r Run) Recording {
	rec := Recording{
		ScenarioID: r.ScenarioID, Scenario: r.Scenario, Protocol: r.Protocol,
		Target: r.Target, Context: r.Context, Outcome: r.Outcome, At: r.At,
	}
	for _, m := range r.Messages {
		if m.Dir == "out" {
			rec.Request = m.Fields
		} else {
			rec.Response = m.Fields
		}
	}
	return rec
}

// Rejudge re-proves a recorded exchange against the casebook with no live target, returning a Run.
// This is replay: the verdict is recomputed from the recorded response, so a change in the casebook
// (or a captured regression) is caught offline and still carries a full, certified proof.
func (e *Engine) Rejudge(rec Recording) Run {
	e.mu.Lock()
	book := e.book
	e.mu.Unlock()
	res := judge(book, verdict.Case{Request: rec.Request, Context: rec.Context, Response: rec.Response})
	res.Certificate = proof.Verify(context.Background(), res.Framework, "pass")
	return Run{
		ScenarioID: rec.ScenarioID, Scenario: rec.Scenario, Protocol: rec.Protocol,
		Target: rec.Target, At: time.Now().UTC(), Outcome: res.Outcome,
		Context: rec.Context, Result: res,
	}
}

// WriteRecordings writes recordings as JSON Lines (one JSON object per line) to path.
func WriteRecordings(path string, recs []Recording) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return w.Flush()
}

// ReadRecordings reads a JSON Lines cassette written by WriteRecordings.
func ReadRecordings(path string) ([]Recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []Recording
	dec := json.NewDecoder(bufio.NewReader(f))
	for {
		var r Recording
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("engine: read recording: %w", err)
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// Replay re-proves every recording in a cassette and aggregates the verdicts like a suite.
func (e *Engine) Replay(recs []Recording) SuiteResult {
	res := SuiteResult{ID: "replay", Name: "replay", At: time.Now().UTC(), Outcome: verdict.Pass}
	start := time.Now()
	for _, rec := range recs {
		run := e.Rejudge(rec)
		run.ID = rec.ScenarioID
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
	return res
}
