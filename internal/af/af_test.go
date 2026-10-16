package af

import "testing"

func TestGroundedChain(t *testing.T) {
	// a -> b -> c : a has no attacker (in), b is attacked by a (out), c is attacked by b (in).
	f := Framework{
		Args:    []Arg{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Attacks: []Attack{{From: "a", To: "b"}, {From: "b", To: "c"}},
	}
	l := f.Grounded()
	if l["a"] != In || l["b"] != Out || l["c"] != In {
		t.Fatalf("grounded labelling wrong: %v", l)
	}
}

func TestGroundedMutualIsUndecided(t *testing.T) {
	// d <-> e with no external support: grounded leaves both undecided.
	f := Framework{
		Args:    []Arg{{ID: "d"}, {ID: "e"}, {ID: "p"}},
		Attacks: []Attack{{From: "d", To: "e"}, {From: "e", To: "d"}, {From: "d", To: "p"}},
	}
	l := f.Grounded()
	if l["d"] != Undec || l["e"] != Undec || l["p"] != Undec {
		t.Fatalf("expected undecided, got %v", l)
	}
}

func TestExplainVisitsDecidingAttackers(t *testing.T) {
	f := Framework{
		Args:    []Arg{{ID: "p", Claim: "pass"}, {ID: "v", Claim: "violation"}},
		Attacks: []Attack{{From: "v", To: "p", Kind: Rebut}},
	}
	steps := f.Explain("p")
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].Status != Out {
		t.Fatalf("pass should be out, got %s", steps[0].Status)
	}
}
