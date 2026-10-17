// Package script compiles a sandboxed scenario DSL into engine scenarios. A script is trusted only
// to *declare* scenarios: it runs in a goja (JavaScript) or gopher-lua (Lua) runtime with no host
// access beyond the scenario() builder and a hard time limit, then prova runs and proves the result
// exactly as it would a built-in scenario. This is the thin DSL that compiles to casebooks.
//
// The DSL is one call:
//
//	scenario("payment over balance", {
//	  protocol: "iso20022",          // message: "pacs.009", "0400", "F" ... (optional)
//	  request:  { Amount: "150.00", Currency: "EUR", DebtorAcct: "ACC-EUR-1", EndToEndId: "E2E-1" },
//	  context:  { availableBalance: "142.00" },
//	  expect:   "reject (AM04)",
//	})
package script

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	lua "github.com/yuin/gopher-lua"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/synth"
)

const timeLimit = 200 * time.Millisecond

// genHolder lets seed() swap the generator while closures keep a stable reference.
type genHolder struct{ g *synth.Gen }

// Compile dispatches to the JavaScript or Lua compiler. lang defaults to JavaScript.
func Compile(lang, src string) ([]engine.Scenario, error) {
	if strings.EqualFold(lang, "lua") {
		return CompileLua(src)
	}
	return CompileJS(src)
}

// CompileJS evaluates a JavaScript scenario script in a sandboxed goja runtime.
func CompileJS(src string) ([]engine.Scenario, error) {
	vm := goja.New()
	var scenarios []engine.Scenario
	var firstErr error

	_ = vm.Set("scenario", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		sc, err := toScenario(name, call.Argument(1).Export())
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return goja.Undefined()
		}
		scenarios = append(scenarios, sc)
		return goja.Undefined()
	})

	// gen: the synthetic data generator; seed() reseeds it for reproducible data-driven runs.
	g := &genHolder{g: synth.New(time.Now().UnixNano())}
	_ = vm.Set("seed", func(n int64) { g.g = synth.New(n) })
	_ = vm.Set("gen", map[string]any{
		"pan":     func(prefix string) string { return g.g.PAN(prefix) },
		"amount":  func(min, max float64) string { return g.g.Amount(min, max) },
		"iban":    func(cc string) string { return g.g.IBAN(cc) },
		"bic":     func(cc string) string { return g.g.BIC(cc) },
		"e2e":     func() string { return g.g.E2E() },
		"account": func() string { return g.g.Account() },
		"stan":    func() string { return g.g.STAN() },
	})

	t := time.AfterFunc(timeLimit, func() { vm.Interrupt("script exceeded time limit") })
	defer t.Stop()

	if _, err := vm.RunString(src); err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return require(scenarios)
}

// CompileLua evaluates a Lua scenario script in a sandboxed gopher-lua state.
func CompileLua(src string) ([]engine.Scenario, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeLimit)
	defer cancel()

	L := lua.NewState(lua.Options{SkipOpenLibs: true}) // no io/os/debug — sandboxed
	defer L.Close()
	L.SetContext(ctx)

	var scenarios []engine.Scenario
	var firstErr error

	L.SetGlobal("scenario", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		tbl := L.CheckTable(2)
		sc, err := toScenario(name, luaTableToMap(tbl))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return 0
		}
		scenarios = append(scenarios, sc)
		return 0
	}))

	g := &genHolder{g: synth.New(time.Now().UnixNano())}
	L.SetGlobal("seed", L.NewFunction(func(L *lua.LState) int { g.g = synth.New(L.CheckInt64(1)); return 0 }))
	genTbl := L.NewTable()
	push := func(name string, fn func(L *lua.LState) int) { L.SetField(genTbl, name, L.NewFunction(fn)) }
	push("pan", func(L *lua.LState) int { L.Push(lua.LString(g.g.PAN(L.OptString(1, "")))); return 1 })
	push("amount", func(L *lua.LState) int {
		L.Push(lua.LString(g.g.Amount(float64(L.CheckNumber(1)), float64(L.CheckNumber(2)))))
		return 1
	})
	push("iban", func(L *lua.LState) int { L.Push(lua.LString(g.g.IBAN(L.OptString(1, "DE")))); return 1 })
	push("bic", func(L *lua.LState) int { L.Push(lua.LString(g.g.BIC(L.OptString(1, "")))); return 1 })
	push("e2e", func(L *lua.LState) int { L.Push(lua.LString(g.g.E2E())); return 1 })
	push("account", func(L *lua.LState) int { L.Push(lua.LString(g.g.Account())); return 1 })
	push("stan", func(L *lua.LState) int { L.Push(lua.LString(g.g.STAN())); return 1 })
	L.SetGlobal("gen", genTbl)

	if err := L.DoString(src); err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return require(scenarios)
}

func require(s []engine.Scenario) ([]engine.Scenario, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("script: no scenario() was declared")
	}
	return s, nil
}

// toScenario converts a declared scenario definition (map form, from either runtime) to an
// engine.Scenario.
func toScenario(name string, def any) (engine.Scenario, error) {
	m, ok := def.(map[string]any)
	if !ok {
		return engine.Scenario{}, fmt.Errorf("scenario %q: definition must be an object", name)
	}
	sc := engine.Scenario{
		ID:       slug(name),
		Name:     name,
		Protocol: str(m["protocol"], "iso20022"),
		Message:  str(m["message"], ""),
		Expect:   str(m["expect"], ""),
		Request:  strMap(m["request"]),
		Context:  strMap(m["context"]),
	}
	if len(sc.Request) == 0 {
		return engine.Scenario{}, fmt.Errorf("scenario %q: a non-empty request is required", name)
	}
	return sc, nil
}

func str(v any, dflt string) string {
	if v == nil {
		return dflt
	}
	return fmt.Sprintf("%v", v)
}

// strMap flattens a nested object/table of scalars into map[string]string.
func strMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = scalar(val)
	}
	return out
}

// scalar renders a DSL scalar without a trailing ".0" for integer-valued floats (JS numbers export
// as float64), so Amount: 150 and Amount: "150" read the same.
func scalar(v any) string {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}

func slug(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return "script-" + strings.Trim(b.String(), "-")
}

// luaTableToMap converts a Lua table to the generic map/value form toScenario understands.
func luaTableToMap(tbl *lua.LTable) map[string]any {
	out := map[string]any{}
	tbl.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			return
		}
		switch val := v.(type) {
		case *lua.LTable:
			out[string(key)] = luaTableToMap(val)
		case lua.LString:
			out[string(key)] = string(val)
		case lua.LNumber:
			out[string(key)] = float64(val)
		case lua.LBool:
			out[string(key)] = bool(val)
		}
	})
	return out
}
