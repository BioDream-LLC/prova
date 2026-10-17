package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/manual"
	"github.com/biodream-llc/prova/internal/sims"
)

// referenceChapters are generated from the running code — the shipped scenarios and suites, the targets, and every
// server option — so the manual cannot describe one that does not exist or omit one that does.
func referenceChapters(eng *engine.Engine, fs *flag.FlagSet) []manual.Chapter {
	var sc [][]string
	for _, s := range eng.Scenarios() {
		sc = append(sc, []string{"`" + s.ID + "`", s.Name, s.Protocol + " " + s.MessageKind(), s.Expect})
	}
	var su [][]string
	for _, s := range eng.Suites() {
		ids := make([]string, len(s.Scenarios))
		for i, id := range s.Scenarios {
			ids[i] = "`" + id + "`"
		}
		su = append(su, []string{"`" + s.ID + "`", s.Name, "`" + s.Target + "`", s.Description + " Runs " + strings.Join(ids, ", ") + "."})
	}
	var tg [][]string
	for _, t := range eng.Targets() {
		tg = append(tg, []string{"`" + t.Name + "`", t.Protocol, t.Note})
	}
	var opts [][]string
	fs.VisitAll(func(f *flag.Flag) {
		def := f.DefValue
		if def == "" || def == "0" || def == "0s" || def == "false" {
			def = "—"
		} else {
			def = "`" + def + "`"
		}
		// The data directory's default is this machine's; the manual states the rule instead.
		if f.Name == "data" {
			def = "see [the database](#the-database)"
		}
		opts = append(opts, []string{"`-" + f.Name + "`", f.Usage, def})
	})
	sort.Slice(opts, func(i, j int) bool { return opts[i][0] < opts[j][0] })

	api := [][]string{
		{"`GET /api/health`", "none", "liveness, for a load balancer"},
		{"`GET /api/version`", "none", "the version"},
		{"`GET /api/auth/providers`", "none", "the sign-in methods offered"},
		{"`POST /api/auth/login`", "none", "sign in with a name and password"},
		{"`POST /api/auth/logout`", "none", "sign out"},
		{"`GET /api/auth/me`", "none", "who is signed in (empty when nobody is)"},
		{"`GET /api/scenarios`", "viewer", "the scenario library"},
		{"`GET /api/targets`", "viewer", "the targets"},
		{"`GET /api/runs`", "viewer", "the recent runs, newest first"},
		{"`GET /api/runs/{id}`", "viewer", "one run with its proof"},
		{"`POST /api/runs`", "operator", "run a scenario, or a script, against a target"},
		{"`GET /api/suites`", "viewer", "the suites"},
		{"`POST /api/suites/run`", "operator", "run a suite"},
		{"`POST /api/load`", "operator", "run a load test"},
		{"`POST /api/transform`", "operator", "convert MT103 ↔ pacs.008"},
		{"`GET /api/reports/summary?days=N`", "viewer", "the reporting summary"},
		{"`GET /api/reports/runs.csv?days=N`", "viewer", "every verdict in the period as CSV"},
		{"`GET /api/users`", "admin", "the accounts"},
		{"`POST /api/users`", "admin", "create or change an account"},
		{"`DELETE /api/users/{name}`", "admin", "remove an account"},
		{"`GET /api/activity`", "admin", "the activity log"},
	}

	return []manual.Chapter{
		{Title: "Scenario Reference", Intro: "Every scenario shipped in this binary. Run one with " +
			"`prova run --scenario ID --target TARGET` or from the Scenarios screen.\n\n" +
			manual.Table([]string{"ID", "Name", "Message", "Expectation"}, sc)},
		{Title: "Suite Reference", Intro: "Every suite shipped in this binary, with its default target.\n\n" +
			manual.Table([]string{"ID", "Name", "Target", "What it covers"}, su)},
		{Title: "Target Reference", Intro: "The simulated targets prova starts unless `-no-sim` is given. Their " +
			"addresses are chosen at start on the loopback interface.\n\n" +
			manual.Table([]string{"Target", "Protocol", "Behaviour"}, tg)},
		{Title: "Server Options", Intro: "Every option the server accepts. Give them as `-name value` or " +
			"`--name=value`.\n\n" + manual.Table([]string{"Option", "Meaning", "Default"}, opts)},
		{Title: "API Reference", Intro: "The HTTP API the interface uses, all of it JSON over HTTPS. A request that " +
			"needs a role must carry a session cookie; a request that changes anything must be " +
			"`application/json` from prova's own origin (see [Security](#security)).\n\n" +
			manual.Table([]string{"Endpoint", "Needs", "Does"}, api)},
	}
}

func buildManual(eng *engine.Engine, fs *flag.FlagSet) (*manual.Doc, error) {
	return manual.Build(version, referenceChapters(eng, fs)...)
}

// registerManual serves the manual at /manual, built once from the running server.
func registerManual(mux *http.ServeMux, eng *engine.Engine, fs *flag.FlagSet) error {
	doc, err := buildManual(eng, fs)
	if err != nil {
		return err
	}
	page := []byte(doc.HTML())
	mux.HandleFunc("GET /manual", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page)
	})
	return nil
}

// manualCLI is `prova manual`: write the manual to a file.
func manualCLI(args []string) int {
	cmd := flag.NewFlagSet("manual", flag.ContinueOnError)
	out := cmd.String("out", "prova-manual.html", "where to write the manual")
	if err := cmd.Parse(args); err != nil {
		return 2
	}
	eng := engine.New()
	// The reference lists the simulated targets a server starts; register them without starting them.
	for _, t := range sims.Specs {
		eng.AddTarget(engine.Target{Name: t.Name, Protocol: t.Protocol, Note: t.Note})
	}
	server := flag.NewFlagSet("prova", flag.ContinueOnError)
	defineServerFlags(server)
	doc, err := buildManual(eng, server)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prova:", err)
		return 1
	}
	if err := os.WriteFile(*out, []byte(doc.HTML()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "prova:", err)
		return 1
	}
	fmt.Printf("wrote %s (%d chapters)\n", *out, len(doc.Chapters))
	return 0
}
