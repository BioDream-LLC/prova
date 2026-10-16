package proof

import (
	"context"
	"testing"

	"github.com/biodream-llc/prova/internal/af"
)

func TestVerifyInProcess(t *testing.T) {
	t.Setenv("PROVA_PROOF", "") // force the in-process verifier

	passOnly := af.Framework{Args: []af.Arg{{ID: "pass"}}}
	c := Verify(context.Background(), passOnly, "pass")
	if c == nil {
		t.Fatal("Verify must never return nil")
	}
	if c.Engine != "builtin-go" {
		t.Fatalf("engine = %q, want builtin-go", c.Engine)
	}
	if c.QueryLabel != "in" || !c.Complete || !c.StableExists || !c.Skeptical || !c.Credulous {
		t.Fatalf("pass-only certificate wrong: %+v", c)
	}

	// pass defeated by an undefeated violation.
	fail := af.Framework{
		Args:    []af.Arg{{ID: "pass"}, {ID: "v"}},
		Attacks: []af.Attack{{From: "v", To: "pass", Kind: af.Rebut}},
	}
	c = Verify(context.Background(), fail, "pass")
	if c.QueryLabel != "out" || !c.Complete {
		t.Fatalf("fail certificate wrong: %+v", c)
	}
	if c.Skeptical || c.Credulous {
		t.Fatalf("defeated query must be neither skeptical nor credulous: %+v", c)
	}
	if !c.StableExists {
		t.Fatalf("a stable extension should exist: %+v", c)
	}
}
