// Command prova serves the prova web interface and runs financial-protocol test scenarios.
//
// prova is a financial protocol test & proof harness: it drives FIX / ISO 8583 / ISO 20022 messages
// at a target (exchange, switch, gateway, OMS/EMS, payment host) and renders a verdict. With the
// prova-proof companion binary attached, each verdict is a formal, cited proof rather than a bare
// pass/fail.
//
// Everything is controlled from the web interface served by this one binary. The interface and the
// manual are embedded, so deployment is a single file: copy it, run it, open the browser. For a
// zero-setup demo it also starts two built-in simulated ISO 20022 hosts as targets.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/biodream-llc/prova/internal/authweb"
	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/mt"
	"github.com/biodream-llc/prova/internal/script"
	"github.com/biodream-llc/prova/internal/sims"
	"github.com/biodream-llc/prova/internal/store"
	"github.com/biodream-llc/prova/internal/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "v0.0.0"

func main() {
	// Subcommand: `prova run` is the headless CI/CD runner.
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Exit(runCLI(os.Args[2:]))
	}
	// Subcommand: `prova user` manages accounts.
	if len(os.Args) > 1 && os.Args[1] == "user" {
		os.Exit(userCLI(os.Args[2:]))
	}
	// Subcommand: `prova manual` writes the reference manual as one HTML file.
	if len(os.Args) > 1 && os.Args[1] == "manual" {
		os.Exit(manualCLI(os.Args[2:]))
	}

	o := defineServerFlags(flag.CommandLine)
	flag.Parse()
	addr, tlsCert, tlsKey, httpRedirect, noSim := o.addr, o.tlsCert, o.tlsKey, o.httpRedirect, o.noSim
	retainDays, demoTraffic, showVersion, af := o.retainDays, o.demoTraffic, o.showVersion, o.auth

	if *showVersion {
		println(version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	st, err := openStore(*af.dataDir)
	if err != nil {
		log.Fatalf("prova: database: %v", err)
	}
	defer st.Close()
	log.Printf("prova: database %s", st.Path())

	signIn, err := setupAuth(ctx, af, st)
	if err != nil {
		log.Fatalf("prova: sign-in: %v", err)
	}

	eng := engine.New()
	if err := eng.Attach(runStore{st}); err != nil {
		log.Fatalf("prova: loading the run history: %v", err)
	}
	if !*noSim {
		startSims(ctx, eng, eng.Stored() == 0)
	}
	if *retainDays > 0 {
		go retain(ctx, st, *retainDays)
	}
	if *demoTraffic > 0 && !*noSim {
		go demoRuns(ctx, eng, *demoTraffic)
	}
	activity := func(r *http.Request, action, detail string) {
		if sess, ok := authweb.SessionFrom(r.Context()); ok {
			if err := st.Log(sess.User, action, detail); err != nil {
				log.Printf("prova: activity log: %v", err)
			}
		}
	}
	// acting marks a request's context with the signed-in account, so its runs record who made them.
	acting := func(r *http.Request) context.Context {
		if sess, ok := authweb.SessionFrom(r.Context()); ok {
			return engine.WithActor(r.Context(), sess.User)
		}
		return r.Context()
	}

	mux := http.NewServeMux()
	signIn.Register(mux)
	registerReports(mux, st)
	if err := registerManual(mux, eng, flag.CommandLine); err != nil {
		log.Fatalf("prova: manual: %v", err)
	}
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"version": version, "webBuilt": web.Available()})
	})
	mux.HandleFunc("GET /api/scenarios", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, eng.Scenarios())
	})
	mux.HandleFunc("GET /api/targets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, eng.Targets())
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, eng.Runs())
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, ok := eng.Get(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		writeJSON(w, http.StatusOK, run)
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scenario string `json:"scenario"`
			Target   string `json:"target"`
			Script   string `json:"script"`
			Lang     string `json:"lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		// A script compiles to one or more scenarios; we run the first against the target.
		if body.Script != "" {
			scs, err := script.Compile(body.Lang, body.Script)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			run, err := eng.RunScenario(acting(r), scs[0], body.Target)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			activity(r, "ran a script", fmt.Sprintf("%s on %s: %s (%s)", run.Scenario, run.Target, run.Outcome, run.ID))
			writeJSON(w, http.StatusOK, run)
			return
		}
		run, err := eng.Run(acting(r), body.Scenario, body.Target)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		activity(r, "ran a scenario", fmt.Sprintf("%s on %s: %s (%s)", run.ScenarioID, run.Target, run.Outcome, run.ID))
		writeJSON(w, http.StatusOK, run)
	})

	mux.HandleFunc("GET /api/suites", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, eng.Suites())
	})
	mux.HandleFunc("POST /api/suites/run", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Suite  string `json:"suite"`
			Target string `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		res, err := eng.RunSuite(acting(r), body.Suite, body.Target)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		activity(r, "ran a suite", fmt.Sprintf("%s on %s: %s, %d pass, %d fail, %d indeterminate", res.ID, res.Target,
			res.Outcome, res.Pass, res.Fail, res.Indeterminate))
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/load", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scenario    string `json:"scenario"`
			Target      string `json:"target"`
			N           int    `json:"n"`
			Concurrency int    `json:"concurrency"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		res, err := eng.RunLoad(acting(r), body.Scenario, body.Target, body.N, body.Concurrency)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		activity(r, "ran a load test", fmt.Sprintf("%s on %s: %d requests, %.0f/s, p95 %.2f ms", body.Scenario, res.Target,
			res.N, res.Throughput, res.P95))
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("POST /api/transform", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Direction string `json:"direction"` // mt2mx | mx2mt
			Payload   string `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		switch body.Direction {
		case "mt2mx":
			req, err := mt.MT103ToPacs008(body.Payload)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			out, err := iso20022.Encode(req)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"result": string(out), "format": "pacs.008"})
		case "mx2mt":
			req, err := iso20022.ParseRequest([]byte(body.Payload))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			out, err := mt.Pacs008ToMT103(req)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"result": out, "format": "MT103"})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "direction must be mt2mx or mx2mt"})
		}
	})

	ui, err := web.Handler()
	if err != nil {
		log.Fatalf("prova: web interface: %v", err)
	}
	mux.Handle("/", ui)

	tlsCfg, certNote, err := tlsConfig(*tlsCert, *tlsKey, *addr)
	if err != nil {
		log.Fatalf("prova: tls: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           secureHeaders(signIn.Guard(mux)),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var redirect *http.Server
	if *httpRedirect != "" {
		redirect = redirectServer(*httpRedirect, *addr)
		go func() {
			log.Printf("prova: redirecting http://%s -> https", *httpRedirect)
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("prova: http redirect: %v", err)
			}
		}()
	}

	go func() {
		log.Printf("prova %s listening on https://%s (web built in: %v)", version, displayAddr(*addr), web.Available())
		log.Printf("prova: %s", certNote)
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("prova: serve: %v", err)
		}
	}()

	<-ctx.Done()

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	if redirect != nil {
		_ = redirect.Shutdown(shutCtx)
	}
	log.Printf("prova: stopped")
}

// serverOptions are the server's command-line options.
type serverOptions struct {
	addr, tlsCert, tlsKey, httpRedirect *string
	noSim, showVersion                  *bool
	retainDays                          *int
	demoTraffic                         *time.Duration
	auth                                *authFlags
}

// defineServerFlags declares the server's options on fs. Kept in one place so the manual's Server Options chapter is
// generated from exactly the options the server accepts.
func defineServerFlags(fs *flag.FlagSet) *serverOptions {
	return &serverOptions{
		addr:         fs.String("addr", ":8443", "address the web interface listens on (HTTPS)"),
		tlsCert:      fs.String("tls-cert", "", "TLS certificate (PEM); if unset prova uses a persistent self-signed certificate"),
		tlsKey:       fs.String("tls-key", "", "TLS private key (PEM), paired with -tls-cert"),
		httpRedirect: fs.String("http-redirect", "", "also listen for plain HTTP on this address and redirect it to HTTPS (e.g. :8080)"),
		noSim:        fs.Bool("no-sim", false, "do not start the built-in simulated targets"),
		retainDays:   fs.Int("retain-days", 0, "delete runs, suite runs, load runs and activity older than this many days (0 keeps everything)"),
		demoTraffic:  fs.Duration("demo-traffic", 0, "run one of the shipped scenarios this often (e.g. 15m), so a demonstration shows live verdicts"),
		showVersion:  fs.Bool("version", false, "print the version and exit"),
		auth:         registerAuthFlags(fs),
	}
}

// startSims launches the built-in simulated targets and registers them, so prova is useful the moment it
// starts with no external system: a compliant and a defective target for each protocol.
func startSims(ctx context.Context, eng *engine.Engine, seedHistory bool) {
	targets, err := sims.Start(ctx)
	if err != nil {
		log.Printf("prova: %v", err)
	}
	for _, t := range targets {
		eng.AddTarget(engine.Target{Name: t.Name, Protocol: t.Protocol, Addr: t.Addr, Note: t.Note})
	}

	if !seedHistory {
		return
	}
	// On a fresh installation, seed the run history so the dashboard shows real verdicts (a pass, a fail, an
	// indeterminate) the moment the interface loads. The runs are real: driven at the simulators and proved.
	for _, s := range seedRuns {
		if _, err := eng.Run(ctx, s.scenario, s.target); err != nil {
			log.Printf("prova: seed run %s@%s: %v", s.scenario, s.target, err)
		}
	}
}

// seedRuns are the first runs of a fresh installation, and the pool a demonstration's traffic is drawn from.
var seedRuns = []struct{ scenario, target string }{
	{"pacs008-within-balance", "cbpr-uat"},
	{"pacs008-over-balance", "cbpr-uat"},
	{"pacs008-over-balance", "switch-sim"},
	{"pacs008-unknown-balance", "cbpr-uat"},
	{"iso8583-over-limit", "card-switch"},
	{"iso8583-within-limit", "card-switch"},
	{"fix-over-limit", "fix-venue"},
	{"fix-within-limit", "fix-venue"},
	{"fix-over-limit", "fix-venue-bug"},
	{"pacs008-currency-mismatch", "cbpr-uat"},
	{"pacs008-duplicate", "cbpr-uat"},
	{"pacs008-back-valued", "cbpr-uat"},
	{"pacs008-invalid-bic", "cbpr-uat"},
	{"pacs004-return-unknown", "switch-sim"},
	{"camt056-cancel-pending", "cbpr-uat"},
	{"pacs028-status-pending", "switch-sim"},
	{"iso8583-expired-card", "card-switch"},
	{"iso8583-reversal-unknown", "card-switch-bug"},
	{"fix-cancel-filled", "fix-venue"},
	{"fix-replace-over", "fix-venue-bug"},
}

// demoRuns runs one of the seed scenarios every interval, so a public demonstration shows live verdicts.
func demoRuns(ctx context.Context, eng *engine.Engine, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s := seedRuns[i%len(seedRuns)]
			if _, err := eng.Run(engine.WithActor(ctx, "demo-traffic"), s.scenario, s.target); err != nil {
				log.Printf("prova: demo traffic %s@%s: %v", s.scenario, s.target, err)
			}
		}
	}
}

// retain deletes history older than days, at start and then daily.
func retain(ctx context.Context, st *store.Store, days int) {
	for {
		n, err := st.Prune(ctx, time.Now().AddDate(0, 0, -days))
		if err != nil {
			log.Printf("prova: retention: %v", err)
		} else if n > 0 {
			log.Printf("prova: retention: removed %d runs older than %d days", n, days)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
