package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/store"
)

// runStore keeps the engine's runs in the database.
type runStore struct{ st *store.Store }

func (r runStore) SaveRun(run engine.Run) error {
	body, err := json.Marshal(run)
	if err != nil {
		return err
	}
	seq, _ := strconv.Atoi(strings.TrimPrefix(run.ID, "r-"))
	return r.st.PutRun(store.Run{ID: run.ID, Seq: seq, At: run.At, ScenarioID: run.ScenarioID, Scenario: run.Scenario,
		Protocol: run.Protocol, Target: run.Target, Outcome: string(run.Outcome), LatencyMs: run.LatencyMs, Actor: run.By,
		SuiteRun: run.SuiteRun, Body: body})
}

func (r runStore) SaveSuite(res engine.SuiteResult, ids []string) error {
	if err := r.st.PutSuiteRun(store.SuiteRun{ID: res.RunID, SuiteID: res.ID, Name: res.Name, Target: res.Target,
		At: res.At, Outcome: string(res.Outcome), Pass: res.Pass, Fail: res.Fail, Indet: res.Indeterminate,
		DurationMs: res.DurationMs, Actor: res.By}); err != nil {
		return err
	}
	return r.st.SetRunSuite(res.RunID, ids)
}

func (r runStore) SaveLoad(lr engine.LoadResult) error {
	body, err := json.Marshal(lr)
	if err != nil {
		return err
	}
	return r.st.PutLoadRun(store.LoadRun{At: lr.At, Scenario: lr.Scenario, Target: lr.Target, N: lr.N,
		Concurrency: lr.Concurrency, Throughput: lr.Throughput, P50: lr.P50, P95: lr.P95, P99: lr.P99, Max: lr.Max,
		Errors: lr.Errors, Actor: lr.By, Body: body})
}

func decodeRun(x store.Run) (engine.Run, error) {
	var run engine.Run
	if err := json.Unmarshal(x.Body, &run); err != nil {
		return engine.Run{}, fmt.Errorf("stored run %s: %w", x.ID, err)
	}
	run.SuiteRun = x.SuiteRun // linked after the run was saved
	return run, nil
}

func (r runStore) RecentRuns(n int) ([]engine.Run, error) {
	xs, err := r.st.RecentRuns(n)
	if err != nil {
		return nil, err
	}
	out := make([]engine.Run, 0, len(xs))
	for _, x := range xs {
		run, err := decodeRun(x)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (r runStore) RunByID(id string) (engine.Run, bool, error) {
	x, ok, err := r.st.RunByID(id)
	if err != nil || !ok {
		return engine.Run{}, false, err
	}
	run, err := decodeRun(x)
	return run, err == nil, err
}

func (r runStore) MaxSeq() (int, error) { return r.st.MaxSeq() }

// --- reports ---------------------------------------------------------------------------------------------------------

type tally struct {
	Pass          int `json:"pass"`
	Fail          int `json:"fail"`
	Indeterminate int `json:"indeterminate"`
}

func (t *tally) add(outcome string) {
	switch outcome {
	case "pass":
		t.Pass++
	case "fail":
		t.Fail++
	default:
		t.Indeterminate++
	}
}

type dayTally struct {
	Day string `json:"day"`
	tally
}

type keyTally struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	tally
	P50 float64 `json:"p50Ms"`
	P95 float64 `json:"p95Ms"`
}

// Report is the reporting summary over a period.
type Report struct {
	From       time.Time        `json:"from"`
	To         time.Time        `json:"to"`
	Days       int              `json:"days"`
	Totals     tally            `json:"totals"`
	Runs       int              `json:"runs"`
	ByDay      []dayTally       `json:"byDay"`
	ByTarget   []keyTally       `json:"byTarget"`
	ByProtocol []keyTally       `json:"byProtocol"`
	ByScenario []keyTally       `json:"byScenario"`
	ByActor    []keyTally       `json:"byActor"`
	Suites     []store.SuiteRun `json:"suites"`
	Loads      []store.LoadRun  `json:"loads"`
	Disagree   int              `json:"engineDisagreements"`
}

func reportDays(r *http.Request) int {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3660 {
		days = 30
	}
	return days
}

func pct(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := p * len(s) / 100
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

func buildReport(st *store.Store, days int, now time.Time) (Report, error) {
	to := now.UTC()
	from := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	runs, err := st.RunsSince(from, true)
	if err != nil {
		return Report{}, err
	}
	rep := Report{From: from, To: to, Days: days, Runs: len(runs)}

	byDay := map[string]*dayTally{}
	for d := 0; d < days; d++ {
		k := from.AddDate(0, 0, d).Format("2006-01-02")
		byDay[k] = &dayTally{Day: k}
		rep.ByDay = append(rep.ByDay, dayTally{})
	}
	type group struct {
		t     keyTally
		lat   []float64
		label string
	}
	groups := map[string]map[string]*group{"target": {}, "protocol": {}, "scenario": {}, "actor": {}}
	bump := func(kind, key, label, outcome string, lat float64) {
		g := groups[kind][key]
		if g == nil {
			g = &group{t: keyTally{Key: key, Label: label}}
			groups[kind][key] = g
		}
		g.t.add(outcome)
		g.lat = append(g.lat, lat)
	}
	for _, x := range runs {
		rep.Totals.add(x.Outcome)
		if d := byDay[x.At.Format("2006-01-02")]; d != nil {
			d.add(x.Outcome)
		}
		actor := x.Actor
		if actor == "" {
			actor = "(system)"
		}
		bump("target", x.Target, "", x.Outcome, x.LatencyMs)
		bump("protocol", x.Protocol, "", x.Outcome, x.LatencyMs)
		bump("scenario", x.ScenarioID, x.Scenario, x.Outcome, x.LatencyMs)
		bump("actor", actor, "", x.Outcome, x.LatencyMs)
		var cc struct {
			Result struct {
				CrossCheck *struct {
					Agrees bool `json:"agrees"`
				} `json:"crossCheck"`
			} `json:"result"`
		}
		if json.Unmarshal(x.Body, &cc) == nil && cc.Result.CrossCheck != nil && !cc.Result.CrossCheck.Agrees {
			rep.Disagree++
		}
	}
	for d := 0; d < days; d++ {
		rep.ByDay[d] = *byDay[from.AddDate(0, 0, d).Format("2006-01-02")]
	}
	flatten := func(kind string) []keyTally {
		out := []keyTally{}
		for _, g := range groups[kind] {
			g.t.P50, g.t.P95 = pct(g.lat, 50), pct(g.lat, 95)
			out = append(out, g.t)
		}
		sort.Slice(out, func(i, j int) bool {
			ni := out[i].Pass + out[i].Fail + out[i].Indeterminate
			nj := out[j].Pass + out[j].Fail + out[j].Indeterminate
			if ni != nj {
				return ni > nj
			}
			return out[i].Key < out[j].Key
		})
		return out
	}
	rep.ByTarget, rep.ByProtocol, rep.ByScenario, rep.ByActor =
		flatten("target"), flatten("protocol"), flatten("scenario"), flatten("actor")
	if rep.Suites, err = st.SuiteRuns(from, 200); err != nil {
		return Report{}, err
	}
	if rep.Loads, err = st.LoadRuns(from, 100); err != nil {
		return Report{}, err
	}
	if rep.Suites == nil {
		rep.Suites = []store.SuiteRun{}
	}
	if rep.Loads == nil {
		rep.Loads = []store.LoadRun{}
	}
	return rep, nil
}

// registerReports adds the reporting endpoints.
func registerReports(mux *http.ServeMux, st *store.Store) {
	mux.HandleFunc("GET /api/reports/summary", func(w http.ResponseWriter, r *http.Request) {
		rep, err := buildReport(st, reportDays(r), time.Now())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rep)
	})
	// runs.csv is every verdict in the period, one row each, for a spreadsheet or an auditor.
	mux.HandleFunc("GET /api/reports/runs.csv", func(w http.ResponseWriter, r *http.Request) {
		days := reportDays(r)
		now := time.Now().UTC()
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
		runs, err := st.RunsSince(from, true)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="prova-runs-%s.csv"`, now.Format("2006-01-02")))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"run", "time_utc", "scenario_id", "scenario", "protocol", "target", "outcome", "latency_ms",
			"finding", "cites", "certificate_engine", "second_engine_agrees", "run_by", "suite_run"})
		for _, x := range runs {
			var b struct {
				Result struct {
					Chain []struct {
						Claim string   `json:"claim"`
						Cites []string `json:"cites"`
					} `json:"chain"`
					Certificate struct {
						Engine string `json:"engine"`
					} `json:"certificate"`
					CrossCheck *struct {
						Agrees bool `json:"agrees"`
					} `json:"crossCheck"`
				} `json:"result"`
			}
			_ = json.Unmarshal(x.Body, &b)
			agree := ""
			if b.Result.CrossCheck != nil {
				agree = strconv.FormatBool(b.Result.CrossCheck.Agrees)
			}
			finding, cites := "", map[string]bool{}
			var citeList []string
			for _, st := range b.Result.Chain {
				finding = st.Claim
				for _, c := range st.Cites {
					if !cites[c] {
						cites[c] = true
						citeList = append(citeList, c)
					}
				}
			}
			_ = cw.Write([]string{x.ID, x.At.Format(time.RFC3339), x.ScenarioID, csvSafe(x.Scenario), x.Protocol,
				csvSafe(x.Target), x.Outcome, strconv.FormatFloat(x.LatencyMs, 'f', 3, 64), csvSafe(finding),
				csvSafe(strings.Join(citeList, "; ")), b.Result.Certificate.Engine, agree, x.Actor, x.SuiteRun})
		}
		cw.Flush()
	})
}

// csvSafe stops a value being read as a formula when the file is opened in a spreadsheet.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
