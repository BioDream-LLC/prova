package script

import (
	"context"
	"testing"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/verdict"
)

const jsOverBalance = `
scenario("payment over balance", {
  protocol: "iso20022",
  request:  { Amount: "150.00", Currency: "EUR", DebtorAcct: "ACC-EUR-1", EndToEndId: "E2E-1" },
  context:  { availableBalance: "142.00" },
  expect:   "reject (AM04)",
})`

const luaOverBalance = `
scenario("payment over balance", {
  protocol = "iso20022",
  request  = { Amount = "150.00", Currency = "EUR", DebtorAcct = "ACC-EUR-1", EndToEndId = "E2E-1" },
  context  = { availableBalance = "142.00" },
  expect   = "reject (AM04)",
})`

func checkScenario(t *testing.T, scs []engine.Scenario) {
	t.Helper()
	if len(scs) != 1 {
		t.Fatalf("want 1 scenario, got %d", len(scs))
	}
	s := scs[0]
	if s.Protocol != "iso20022" || s.Request["Amount"] != "150.00" || s.Context["availableBalance"] != "142.00" {
		t.Fatalf("scenario did not compile correctly: %+v", s)
	}
}

func TestCompileJS(t *testing.T) {
	scs, err := CompileJS(jsOverBalance)
	if err != nil {
		t.Fatal(err)
	}
	checkScenario(t, scs)
}

func TestCompileLua(t *testing.T) {
	scs, err := CompileLua(luaOverBalance)
	if err != nil {
		t.Fatal(err)
	}
	checkScenario(t, scs)
}

func TestCompileErrors(t *testing.T) {
	if _, err := CompileJS(`1 + 1`); err == nil {
		t.Fatal("expected error when no scenario declared")
	}
	if _, err := CompileJS(`scenario("empty", {})`); err == nil {
		t.Fatal("expected error when request is missing")
	}
	if _, err := CompileJS(`this is not valid javascript (`); err == nil {
		t.Fatal("expected a syntax error")
	}
}

func TestNumberCoercion(t *testing.T) {
	scs, err := CompileJS(`scenario("n", { request: { Amount: 150 } })`)
	if err != nil {
		t.Fatal(err)
	}
	if scs[0].Request["Amount"] != "150" {
		t.Fatalf("integer amount should render as \"150\", got %q", scs[0].Request["Amount"])
	}
}

func TestGenAndDataDriven(t *testing.T) {
	// A real JS loop + the generator = data-driven scenarios, reproducible via seed().
	src := `
seed(7)
for (let i = 0; i < 5; i++) {
  scenario("gen " + i, {
    request: { Amount: gen.amount(10, 20), PAN: gen.pan("4"), EndToEndId: gen.e2e() },
    context: { availableBalance: "100.00" },
  })
}`
	scs, err := CompileJS(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(scs) != 5 {
		t.Fatalf("data-driven loop should yield 5 scenarios, got %d", len(scs))
	}
	for _, s := range scs {
		pan := s.Request["PAN"]
		if len(pan) != 16 {
			t.Fatalf("generated PAN wrong length: %q", pan)
		}
	}
	// Reproducibility: same seed, same first PAN.
	again, _ := CompileJS(src)
	if again[0].Request["PAN"] != scs[0].Request["PAN"] {
		t.Fatal("seeded generation must be reproducible")
	}
}

func TestGenLua(t *testing.T) {
	scs, err := CompileLua(`seed(1); scenario("lua gen", { request = { PAN = gen.pan("5"), Amount = gen.amount(1, 2) } })`)
	if err != nil {
		t.Fatal(err)
	}
	if len(scs[0].Request["PAN"]) != 16 {
		t.Fatalf("lua gen.pan wrong: %q", scs[0].Request["PAN"])
	}
}

func TestCompiledScenarioRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sim := &iso20022.Sim{Ledger: map[string]int64{"ACC-EUR-1": 14200}, Mode: "accept-all"}
	addr, err := sim.Serve(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	eng := engine.New()
	eng.AddTarget(engine.Target{Name: "bug", Protocol: "iso20022", Addr: addr})

	scs, err := CompileJS(jsOverBalance)
	if err != nil {
		t.Fatal(err)
	}
	run, err := eng.RunScenario(ctx, scs[0], "bug")
	if err != nil {
		t.Fatal(err)
	}
	// The accept-all host accepts a payment over balance; prova must prove a failure.
	if run.Outcome != verdict.Fail {
		t.Fatalf("compiled scenario outcome = %s, want fail", run.Outcome)
	}
}
