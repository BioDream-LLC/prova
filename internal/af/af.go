// Package af is prova's argumentation core: an abstract argumentation framework (Dung) plus the
// grounded-extension solver that turns a set of arguments and attacks into a decided, explainable
// verdict.
//
// A verdict in prova is not an opinion. The casebook compiles the rule and the observed evidence
// into a Framework; this package computes which arguments are accepted under grounded semantics
// (the least fixed point of the characteristic function) and emits the cited chain that proves it.
// The prova-proof C++ companion re-checks the same Framework with a SAT backend for stronger
// (stable/preferred) semantics and a machine-checkable certificate; the result here is the fallback
// that lets prova decide and explain with no companion attached.
package af

import "sort"

// Kind classifies an attack for display and for the proof narrative.
//
//	rebut     — attacks another argument's conclusion head-on
//	undermine — attacks a premise/evidence the argument relies on
//	undercut  — attacks the inference link, not the premise or conclusion
type Kind string

const (
	Rebut     Kind = "rebut"
	Undermine Kind = "undermine"
	Undercut  Kind = "undercut"
)

// Arg is one argument (a claim with its citations).
type Arg struct {
	ID    string   `json:"id"`
	Claim string   `json:"claim"`
	Cites []string `json:"cites,omitempty"`
}

// Attack is a directed attack From defeats To.
type Attack struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind Kind   `json:"kind"`
}

// Framework is a Dung abstract argumentation framework.
type Framework struct {
	Args    []Arg    `json:"args"`
	Attacks []Attack `json:"attacks"`
}

// Label is the grounded status of an argument.
type Label string

const (
	In    Label = "in"    // accepted: every attacker is out
	Out   Label = "out"   // defeated: some attacker is in
	Undec Label = "undec" // undecided: neither holds — the evidence does not settle it
)

// attackersOf returns the IDs of arguments that attack id.
func (f Framework) attackersOf(id string) []string {
	var out []string
	for _, a := range f.Attacks {
		if a.To == id {
			out = append(out, a.From)
		}
	}
	return out
}

// Grounded computes the grounded labelling: the least fixed point of the characteristic function.
// An argument is In when all of its attackers are Out; Out when at least one attacker is In;
// everything that never settles is Undec. Grounded is unique and always exists.
func (f Framework) Grounded() map[string]Label {
	label := make(map[string]Label, len(f.Args))
	for _, a := range f.Args {
		label[a.ID] = Undec
	}
	for {
		changed := false
		for _, a := range f.Args {
			if label[a.ID] != Undec {
				continue
			}
			atk := f.attackersOf(a.ID)
			allOut, anyIn := true, false
			for _, at := range atk {
				switch label[at] {
				case In:
					anyIn = true
					allOut = false
				case Undec:
					allOut = false
				case Out:
					// an attacker that is out does not threaten this argument
				}
			}
			if allOut { // includes the no-attacker case
				label[a.ID] = In
				changed = true
			} else if anyIn {
				label[a.ID] = Out
				changed = true
			}
		}
		if !changed {
			return label
		}
	}
}

// Step is one line of the proof: a claim, why it holds, and what it cites.
type Step struct {
	Claim   string   `json:"claim"`
	Status  Label    `json:"status"`
	Because string   `json:"because"`
	Cites   []string `json:"cites,omitempty"`
}

// Explain produces the cited proof chain for a query argument under the grounded labelling,
// walking from the query through the attackers that decide it. The chain is deterministic.
func (f Framework) Explain(query string) []Step {
	label := f.Grounded()
	byID := make(map[string]Arg, len(f.Args))
	for _, a := range f.Args {
		byID[a.ID] = a
	}
	var steps []Step
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		a, ok := byID[id]
		if !ok {
			return
		}
		atk := f.attackersOf(id)
		sort.Strings(atk)
		var because string
		switch label[id] {
		case In:
			if len(atk) == 0 {
				because = "established directly by evidence; nothing attacks it"
			} else {
				because = "accepted — every argument against it is itself defeated"
			}
		case Out:
			for _, at := range atk {
				if label[at] == In {
					because = "defeated by " + quote(byID[at].Claim)
					break
				}
			}
		default:
			because = "undecided — the evidence does not settle it either way"
		}
		steps = append(steps, Step{Claim: a.Claim, Status: label[id], Because: because, Cites: a.Cites})
		// Recurse into the attackers that matter to this argument's status.
		for _, at := range atk {
			if label[id] == In && label[at] == Out {
				walk(at)
			} else if label[id] == Out && label[at] == In {
				walk(at)
			} else if label[id] == Undec {
				walk(at)
			}
		}
	}
	walk(query)
	return steps
}

func quote(s string) string { return "\u201c" + s + "\u201d" }

// Complete reports whether a labelling is a complete labelling: every argument's label is justified
// by its attackers (in ⇔ all attackers out; out ⇔ some attacker in; undec otherwise). The grounded
// labelling is always complete; this is the check a certificate re-verifies.
func (f Framework) Complete(label map[string]Label) bool {
	for _, a := range f.Args {
		atk := f.attackersOf(a.ID)
		allOut, anyIn := true, false
		for _, at := range atk {
			switch label[at] {
			case In:
				anyIn = true
				allOut = false
			case Undec:
				allOut = false
			}
		}
		switch label[a.ID] {
		case In:
			if !allOut {
				return false
			}
		case Out:
			if !anyIn {
				return false
			}
		default: // Undec: must not be forced either way
			if allOut || anyIn {
				return false
			}
		}
	}
	return true
}

// stableCap bounds the brute-force extension search; larger frameworks are left to the SAT-backed
// companion.
const stableCap = 16

// CompleteExtensions enumerates the complete extensions (as sorted argument-id sets): the IN-sets of
// the complete labellings. A conflict-free set S is complete iff it equals the set of arguments it
// defends (every attacker of a candidate is itself attacked by S). Returns nil when the framework is
// too large for the brute-force search. The grounded extension is the ⊆-least of these.
func (f Framework) CompleteExtensions() [][]string {
	n := len(f.Args)
	if n > stableCap {
		return nil
	}
	idx := make(map[string]int, n)
	for i, a := range f.Args {
		idx[a.ID] = i
	}
	atk := make([]uint32, n) // atk[i] bit j set if j attacks i
	out := make([]uint32, n) // out[i] bit j set if i attacks j
	for _, a := range f.Attacks {
		fi, ok1 := idx[a.From]
		ti, ok2 := idx[a.To]
		if ok1 && ok2 {
			atk[ti] |= 1 << uint(fi)
			out[fi] |= 1 << uint(ti)
		}
	}
	defended := func(mask uint32) uint32 {
		var d uint32
		for i := 0; i < n; i++ {
			ok := true
			for j := 0; j < n; j++ {
				if atk[i]&(1<<uint(j)) != 0 && atk[j]&mask == 0 { // attacker j not attacked by mask
					ok = false
					break
				}
			}
			if ok {
				d |= 1 << uint(i)
			}
		}
		return d
	}
	var exts [][]string
	for mask := uint32(0); mask < (1 << uint(n)); mask++ {
		conflictFree := true
		for i := 0; i < n && conflictFree; i++ {
			if mask&(1<<uint(i)) != 0 && out[i]&mask != 0 {
				conflictFree = false
			}
		}
		if !conflictFree {
			continue
		}
		if defended(mask) != mask { // complete iff S defends exactly itself
			continue
		}
		var set []string
		for i := 0; i < n; i++ {
			if mask&(1<<uint(i)) != 0 {
				set = append(set, f.Args[i].ID)
			}
		}
		sort.Strings(set)
		exts = append(exts, set)
	}
	return exts
}

// GroundedVerified independently cross-checks the grounded label of query against the complete
// extensions: an IN query must be in every complete extension, an OUT query in none, and an UNDEC
// query in some but not all. Returns false if the framework is too large to enumerate.
func (f Framework) GroundedVerified(query string) bool {
	exts := f.CompleteExtensions()
	if exts == nil {
		return false
	}
	label := f.Grounded()[query]
	inSome, inAll := false, true
	for _, s := range exts {
		has := false
		for _, id := range s {
			if id == query {
				has = true
				break
			}
		}
		if has {
			inSome = true
		} else {
			inAll = false
		}
	}
	switch label {
	case In:
		return inAll
	case Out:
		return !inSome
	default:
		return inSome && !inAll
	}
}

// Stable enumerates the stable extensions (as sorted argument-id sets): conflict-free sets that
// attack every argument outside them. Returns nil (not an empty slice) when the framework is too
// large for the brute-force search.
func (f Framework) Stable() [][]string {
	n := len(f.Args)
	if n > stableCap {
		return nil
	}
	idx := make(map[string]int, n)
	for i, a := range f.Args {
		idx[a.ID] = i
	}
	// attack bitmask: atk[i] has bit j set if j attacks i.
	atk := make([]uint32, n)
	out := make([]uint32, n) // out[i] bit j set if i attacks j
	for _, a := range f.Attacks {
		fi, ok1 := idx[a.From]
		ti, ok2 := idx[a.To]
		if ok1 && ok2 {
			atk[ti] |= 1 << uint(fi)
			out[fi] |= 1 << uint(ti)
		}
	}
	var exts [][]string
	for mask := uint32(0); mask < (1 << uint(n)); mask++ {
		conflictFree := true
		for i := 0; i < n && conflictFree; i++ {
			if mask&(1<<uint(i)) != 0 && out[i]&mask != 0 {
				conflictFree = false
			}
		}
		if !conflictFree {
			continue
		}
		// every argument outside mask must be attacked by mask
		stable := true
		for i := 0; i < n && stable; i++ {
			if mask&(1<<uint(i)) == 0 && atk[i]&mask == 0 {
				stable = false
			}
		}
		if !stable {
			continue
		}
		var set []string
		for i := 0; i < n; i++ {
			if mask&(1<<uint(i)) != 0 {
				set = append(set, f.Args[i].ID)
			}
		}
		sort.Strings(set)
		exts = append(exts, set)
	}
	return exts
}
