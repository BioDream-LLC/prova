// Package engine runs scenarios: it drives a protocol adapter at a target, records the message
// exchange, hands the result to the casebook and keeps the decided runs for the interface. It is
// protocol-agnostic — it speaks only the adapter and verdict SPIs.
package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/biodream-llc/prova/internal/adapter"
	"github.com/biodream-llc/prova/internal/proof"
	"github.com/biodream-llc/prova/internal/verdict"
)

// Scenario is a single scripted exchange: a request to send, the state the tester has seeded (and
// therefore knows), and a human-readable expectation.
type Scenario struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Protocol    string            `json:"protocol"`
	Message     string            `json:"message,omitempty"` // the message sent: pacs.008, 0400, F ...; empty is the protocol's default
	Description string            `json:"description"`
	Request     map[string]string `json:"request"`
	Context     map[string]string `json:"context"`
	Expect      string            `json:"expect"`
}

// Message is one leg of the exchange, normalized for display.
type Message struct {
	Dir       string            `json:"dir"` // out | in
	Kind      string            `json:"kind"`
	Summary   string            `json:"summary"`
	Fields    map[string]string `json:"fields"`
	Raw       string            `json:"raw"`
	LatencyMs float64           `json:"latencyMs,omitempty"`
}

// Target is a configured place to drive traffic at.
type Target struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Addr     string `json:"addr"`
	Note     string `json:"note,omitempty"`
}

// Run is a completed scenario execution and its proved verdict.
type Run struct {
	ID         string            `json:"id"`
	ScenarioID string            `json:"scenarioId"`
	Scenario   string            `json:"scenario"`
	Protocol   string            `json:"protocol"`
	Target     string            `json:"target"`
	At         time.Time         `json:"at"`
	Outcome    verdict.Outcome   `json:"outcome"`
	LatencyMs  float64           `json:"latencyMs"` // round trip, to the microsecond
	Context    map[string]string `json:"context,omitempty"`
	Messages   []Message         `json:"messages"`
	Result     verdict.Result    `json:"result"`
	By         string            `json:"by,omitempty"`       // the account that ran it
	SuiteRun   string            `json:"suiteRun,omitempty"` // the suite execution it was part of
}

// Store keeps runs beyond the in-memory window and across restarts. It is optional: without one the engine keeps the
// most recent runs in memory only.
type Store interface {
	SaveRun(Run) error
	SaveSuite(res SuiteResult, runIDs []string) error
	SaveLoad(LoadResult) error
	RecentRuns(n int) ([]Run, error) // newest first
	RunByID(id string) (Run, bool, error)
	MaxSeq() (int, error)
}

type actorKey struct{}

// WithActor marks ctx as acting for an account, so the runs it makes record who made them.
func WithActor(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, actorKey{}, name)
}

func actorFrom(ctx context.Context) string {
	s, _ := ctx.Value(actorKey{}).(string)
	return s
}

// Engine holds scenarios, targets and the recent-run history.
type Engine struct {
	mu        sync.Mutex
	seq       int
	runs      []Run
	targets   map[string]Target
	order     []string
	scenarios []Scenario
	suites    []Suite
	book      verdict.Casebook
	st        Store
}

// memoryRuns is how many recent runs are kept in memory for the interface.
const memoryRuns = 500

// Attach makes st the engine's store: the recent runs are loaded from it, numbering continues after its last run, and
// every new run, suite run and load run is saved to it.
func (e *Engine) Attach(st Store) error {
	recent, err := st.RecentRuns(memoryRuns)
	if err != nil {
		return err
	}
	seq, err := st.MaxSeq()
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.st = st
	e.runs = e.runs[:0]
	for i := len(recent) - 1; i >= 0; i-- {
		e.runs = append(e.runs, recent[i])
	}
	if seq > e.seq {
		e.seq = seq
	}
	return nil
}

// Stored is how many runs the engine holds in memory (all of them, without a store).
func (e *Engine) Stored() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.runs)
}

// New builds an engine seeded with the shipped scenarios, suites and casebook.
func New() *Engine {
	return &Engine{
		seq:       1040,
		targets:   map[string]Target{},
		book:      verdict.Default(),
		scenarios: builtinScenarios(),
		suites:    builtinSuites(),
	}
}

// AddTarget registers a target the interface can run scenarios against.
func (e *Engine) AddTarget(t Target) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.targets[t.Name]; !ok {
		e.order = append(e.order, t.Name)
	}
	e.targets[t.Name] = t
}

// Targets lists registered targets in registration order.
func (e *Engine) Targets() []Target {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Target, 0, len(e.order))
	for _, n := range e.order {
		out = append(out, e.targets[n])
	}
	return out
}

// Scenarios lists the shipped scenarios.
func (e *Engine) Scenarios() []Scenario {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Scenario(nil), e.scenarios...)
}

// Runs returns the recent runs, newest first.
func (e *Engine) Runs() []Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Run, len(e.runs))
	for i, r := range e.runs {
		out[len(e.runs)-1-i] = r
	}
	return out
}

// Get returns a run by id, from memory or, for an older run, from the store.
func (e *Engine) Get(id string) (Run, bool) {
	e.mu.Lock()
	st := e.st
	for _, r := range e.runs {
		if r.ID == id {
			e.mu.Unlock()
			return r, true
		}
	}
	e.mu.Unlock()
	if st != nil {
		if r, ok, err := st.RunByID(id); err == nil && ok {
			return r, true
		}
	}
	return Run{}, false
}

func (e *Engine) scenario(id string) (Scenario, bool) {
	for _, s := range e.scenarios {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

// Run executes scenarioID (from the registry) against targetName.
func (e *Engine) Run(ctx context.Context, scenarioID, targetName string) (Run, error) {
	e.mu.Lock()
	sc, ok := e.scenario(scenarioID)
	e.mu.Unlock()
	if !ok {
		return Run{}, fmt.Errorf("engine: unknown scenario %q", scenarioID)
	}
	return e.RunScenario(ctx, sc, targetName)
}

// RunScenario executes an arbitrary scenario against targetName, stores the run, and returns it.
func (e *Engine) RunScenario(ctx context.Context, sc Scenario, targetName string) (Run, error) {
	run, err := e.execOnce(ctx, sc, targetName)
	if err != nil {
		return Run{}, err
	}
	run.By = actorFrom(ctx)
	if err := e.store(&run); err != nil {
		return Run{}, err
	}
	return run, nil
}

// execOnce drives one scenario at a target and judges it, without storing the run.
func (e *Engine) execOnce(ctx context.Context, sc Scenario, targetName string) (Run, error) {
	e.mu.Lock()
	tgt, okT := e.targets[targetName]
	book := e.book
	e.mu.Unlock()
	if !okT {
		return Run{}, fmt.Errorf("engine: unknown target %q", targetName)
	}

	a, ok := adapter.Get(sc.Protocol)
	if !ok {
		return Run{}, fmt.Errorf("engine: no adapter for protocol %q", sc.Protocol)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := a.Connect(ctx, adapter.Target{Addr: tgt.Addr}, adapter.SessionConfig{})
	if err != nil {
		return Run{}, err
	}
	defer conn.Close()

	msg, err := conn.Encode(adapter.Template{Name: sc.MessageKind()}, sc.Request)
	if err != nil {
		return Run{}, err
	}
	h, err := conn.Send(ctx, msg)
	if err != nil {
		return Run{}, err
	}
	dec, err := conn.Await(ctx, h)
	if err != nil {
		return Run{}, err
	}

	lat, _ := strconv.ParseFloat(dec.Meta["latencyMs"], 64)
	res := judge(book, verdict.Case{Request: sc.Request, Context: sc.Context, Response: dec.Fields})
	// Independently certify the framework — via the prova-proof companion if attached, else in-process.
	res.Certificate = proof.Verify(ctx, res.Framework, "pass")

	outKind := messageName(sc.Protocol, sc.MessageKind())
	inKind := responseName(sc.Protocol, dec.Fields)
	messages := []Message{
		{
			Dir:     "out",
			Kind:    outKind,
			Summary: requestSummary(outKind, sc.Request),
			Fields:  sc.Request,
			Raw:     string(msg),
		},
		{
			Dir:       "in",
			Kind:      inKind,
			Summary:   responseSummary(sc.Protocol, dec.Fields),
			Fields:    dec.Fields,
			Raw:       string(dec.Raw),
			LatencyMs: lat,
		},
	}

	return Run{
		ScenarioID: sc.ID,
		Scenario:   sc.Name,
		Protocol:   sc.Protocol,
		Target:     tgt.Name,
		At:         time.Now().UTC(),
		Outcome:    res.Outcome,
		LatencyMs:  lat,
		Context:    sc.Context,
		Messages:   messages,
		Result:     res,
	}, nil
}

// store assigns an id, saves the run to the store if there is one, and keeps it in the bounded in-memory history.
func (e *Engine) store(run *Run) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	run.ID = fmt.Sprintf("r-%d", e.seq)
	if e.st != nil {
		if err := e.st.SaveRun(*run); err != nil {
			return fmt.Errorf("engine: saving run %s: %w", run.ID, err)
		}
	}
	e.runs = append(e.runs, *run)
	if len(e.runs) > memoryRuns {
		e.runs = e.runs[len(e.runs)-memoryRuns:]
	}
	return nil
}

// MessageKind is the message the scenario sends: its Message, or the protocol's default.
func (sc Scenario) MessageKind() string {
	if sc.Message != "" {
		return sc.Message
	}
	switch sc.Protocol {
	case "iso8583":
		return "0100"
	case "fix":
		return "D"
	}
	return "pacs.008"
}

// fixNames are the FIX messages prova sends and receives, by MsgType.
var fixNames = map[string]string{
	"D": "NewOrderSingle(D)", "F": "OrderCancelRequest(F)", "G": "OrderCancelReplaceRequest(G)",
	"H": "OrderStatusRequest(H)", "8": "ExecutionReport(8)", "9": "OrderCancelReject(9)", "3": "Reject(3)",
}

func messageName(protocol, kind string) string {
	if protocol == "fix" {
		if n, ok := fixNames[kind]; ok {
			return n
		}
	}
	return kind
}

func responseName(protocol string, f map[string]string) string {
	switch protocol {
	case "iso8583":
		return f["MTI"]
	case "fix":
		return messageName("fix", f["35"])
	}
	if _, ok := f["Conf"]; ok {
		return "camt.029"
	}
	return "pacs.002"
}

func requestSummary(kind string, r map[string]string) string {
	s := kind
	if a := r["Amount"]; a != "" {
		s += " " + a + " " + r["Currency"]
	}
	for _, k := range []string{"OrgnlEndToEndId", "OriginalSTAN", "OrigClOrdID"} {
		if v := r[k]; v != "" {
			s += " re " + v
		}
	}
	return s
}

func responseSummary(protocol string, f map[string]string) string {
	switch protocol {
	case "iso8583":
		return f["MTI"] + " DE39=" + f["DE39"]
	case "fix":
		switch {
		case f["35"] == "9":
			return "OrderCancelReject CxlRejReason=" + f["102"]
		case f["decision"] == "rejected":
			return "ExecReport Rejected / OrdRejReason=" + f["rejectReason"]
		case f["150"] == "I":
			return "ExecReport status OrdStatus=" + f["39"]
		}
		return "ExecReport ExecType=" + f["150"] + " OrdStatus=" + f["39"]
	default:
		kind, sts := "pacs.002 ", f["TxSts"]
		if c, ok := f["Conf"]; ok {
			kind, sts = "camt.029 ", c
		}
		s := kind + sts
		if r := f["rejectReason"]; r != "" {
			s += " / " + r
		}
		return s
	}
}
