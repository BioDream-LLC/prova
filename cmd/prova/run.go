package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"

	"github.com/biodream-llc/prova/internal/engine"
)

// runCLI is `prova run` — the headless runner for CI/CD. It executes a suite, a single scenario, or
// a load test against the built-in simulated targets, writes optional JUnit/JSON reports, and exits
// non-zero when a verdict fails (and, unless allowed, when one is indeterminate).
//
//	prova run --suite regression-iso20022 --junit results.xml
//	prova run --scenario iso8583-over-limit --target card-switch
//	prova run --scenario pacs008-within-balance --target cbpr-uat --load 500 --concurrency 16
func runCLI(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	suite := fs.String("suite", "", "suite id to run")
	scenario := fs.String("scenario", "", "scenario id to run")
	target := fs.String("target", "", "target name (overrides a suite's default)")
	junit := fs.String("junit", "", "write a JUnit XML report to this file")
	jsonOut := fs.String("json", "", "write a JSON report to this file")
	load := fs.Int("load", 0, "run the scenario this many times as a load test")
	concurrency := fs.Int("concurrency", 8, "load-test concurrency")
	record := fs.String("record", "", "write a replayable cassette (JSONL) of the runs to this file")
	replay := fs.String("replay", "", "re-prove a recorded cassette (JSONL) offline, with no live target")
	allowIndet := fs.Bool("allow-indeterminate", false, "treat indeterminate as success")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	eng := engine.New()

	// Replay re-proves a cassette offline; it needs no simulated targets.
	if *replay != "" {
		recs, err := engine.ReadRecordings(*replay)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		res := eng.Replay(recs)
		res.Name = "replay " + *replay
		printSuite(res)
		writeReport(*jsonOut, *junit, nil, &res)
		return suiteExit(res, *allowIndet)
	}

	startSims(ctx, eng, false)

	switch {
	case *load > 0:
		if *scenario == "" || *target == "" {
			fmt.Fprintln(os.Stderr, "prova run --load requires --scenario and --target")
			return 2
		}
		lr, err := eng.RunLoad(ctx, *scenario, *target, *load, *concurrency)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		fmt.Printf("load %s @ %s: %d requests, %d workers in %dms\n", lr.Scenario, lr.Target, lr.N, lr.Concurrency, lr.DurationMs)
		fmt.Printf("  throughput %.0f req/s   latency p50=%.2fms p95=%.2fms p99=%.2fms max=%.2fms   errors=%d\n",
			lr.Throughput, lr.P50, lr.P95, lr.P99, lr.Max, lr.Errors)
		fmt.Printf("  outcomes: %v\n", lr.Outcomes)
		writeReport(*jsonOut, "", lr, nil)
		if lr.Errors > 0 || lr.Outcomes["fail"] > 0 {
			return 1
		}
		return 0

	case *suite != "":
		res, err := eng.RunSuite(ctx, *suite, *target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		printSuite(res)
		writeReport(*jsonOut, *junit, nil, &res)
		writeCassette(*record, res.Runs)
		return suiteExit(res, *allowIndet)

	case *scenario != "":
		if *target == "" {
			fmt.Fprintln(os.Stderr, "prova run --scenario requires --target")
			return 2
		}
		run, err := eng.Run(ctx, *scenario, *target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		fmt.Printf("%-7s %-40s %-12s %s\n", run.ID, run.Scenario, run.Target, run.Outcome)
		res := engine.SuiteResult{ID: "single", Name: run.Scenario, Target: run.Target, Runs: []engine.Run{run}, Outcome: run.Outcome}
		switch run.Outcome {
		case "pass":
			res.Pass = 1
		case "fail":
			res.Fail = 1
		default:
			res.Indeterminate = 1
		}
		writeReport(*jsonOut, *junit, nil, &res)
		writeCassette(*record, res.Runs)
		return suiteExit(res, *allowIndet)

	default:
		fmt.Fprintln(os.Stderr, "usage: prova run [--suite ID | --scenario ID --target NAME] [--junit f] [--json f] [--load N]")
		return 2
	}
}

func suiteExit(res engine.SuiteResult, allowIndet bool) int {
	if res.Fail > 0 {
		return 1
	}
	if res.Indeterminate > 0 && !allowIndet {
		return 2
	}
	return 0
}

// writeCassette records the runs as a replayable JSONL cassette when a path is given.
func writeCassette(path string, runs []engine.Run) {
	if path == "" {
		return
	}
	recs := make([]engine.Recording, 0, len(runs))
	for _, r := range runs {
		recs = append(recs, engine.RecordingFrom(r))
	}
	if err := engine.WriteRecordings(path, recs); err != nil {
		fmt.Fprintln(os.Stderr, "prova: record:", err)
	}
}

func printSuite(res engine.SuiteResult) {
	fmt.Printf("suite %s @ %s\n", res.Name, res.Target)
	disagree := 0
	for _, r := range res.Runs {
		second := ""
		if cc := r.Result.CrossCheck; cc != nil {
			if cc.Agrees {
				second = "quorum agrees"
			} else {
				second = "QUORUM DISAGREES: " + string(cc.Outcome)
				disagree++
			}
		}
		fmt.Printf("  %-7s %-40s %-14s %s\n", r.ID, r.Scenario, r.Outcome, second)
	}
	fmt.Printf("  => %s  (%d pass, %d fail, %d indeterminate, %dms; engines disagree on %d)\n",
		res.Outcome, res.Pass, res.Fail, res.Indeterminate, res.DurationMs, disagree)
}

func writeReport(jsonPath, junitPath string, load any, suite *engine.SuiteResult) {
	if jsonPath != "" {
		var v any = load
		if suite != nil {
			v = suite
		}
		if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			_ = os.WriteFile(jsonPath, b, 0o644)
		}
	}
	if junitPath != "" && suite != nil {
		if b, err := xml.MarshalIndent(toJUnit(*suite), "", "  "); err == nil {
			_ = os.WriteFile(junitPath, append([]byte(xml.Header), b...), 0o644)
		}
	}
}

// --- JUnit XML ---

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     float64     `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitMessage `xml:"failure,omitempty"`
	Skipped   *junitMessage `xml:"skipped,omitempty"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func toJUnit(res engine.SuiteResult) junitSuites {
	ts := junitSuite{Name: res.Name, Tests: len(res.Runs), Failures: res.Fail, Skipped: res.Indeterminate, Time: float64(res.DurationMs) / 1000}
	for _, r := range res.Runs {
		c := junitCase{Name: r.Scenario, Classname: "prova." + r.Target}
		switch r.Outcome {
		case "fail":
			c.Failure = &junitMessage{Message: "verdict: fail", Text: proofText(r)}
		case "indeterminate":
			c.Skipped = &junitMessage{Message: "verdict: indeterminate — " + join(r.Result.Settles)}
		}
		ts.Cases = append(ts.Cases, c)
	}
	return junitSuites{Suites: []junitSuite{ts}}
}

func proofText(r engine.Run) string {
	var s string
	for _, step := range r.Result.Chain {
		s += "- " + step.Claim + " [" + string(step.Status) + "] — " + step.Because + "\n"
	}
	return s
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
