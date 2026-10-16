// Package verdict defines the verdict-backend SPI and the in-core builtin backend.
//
// A verdict backend judges a scenario step from what came back and what the backend state became.
// The core depends only on the Backend interface, never on the C++ proof engine: prova ships a
// trivial Go builtin here, and the prova-proof companion attaches over IPC as a second Backend that
// returns a formal, cited proof (and an Indeterminate outcome where the evidence does not decide).
package verdict

import "context"

// Outcome is the result of judging a step.
type Outcome string

const (
	Pass          Outcome = "pass"
	Fail          Outcome = "fail"
	Indeterminate Outcome = "indeterminate" // only the proof backend produces this
)

// Step is one line of a proof: a claim, why it holds, and what it cites.
type Step struct {
	Claim   string   `json:"claim"`
	Because string   `json:"because"`
	Cites   []string `json:"cites,omitempty"`
}

// Verdict is what a backend returns.
type Verdict struct {
	Outcome Outcome  `json:"outcome"`
	Proof   []Step   `json:"proof,omitempty"`   // empty for builtin; a cited chain for prova-proof
	Settles []string `json:"settles,omitempty"` // for Indeterminate: observations that would decide it
}

// Observations are what a backend judges: the decoded response plus any recorded state deltas.
type Observations struct {
	StepID   string            `json:"stepId"`
	Response map[string]string `json:"response"`
	State    map[string]string `json:"state,omitempty"`
}

// Backend judges observations into a verdict.
type Backend interface {
	Judge(ctx context.Context, obs Observations) (Verdict, error)
}

// Builtin is the in-core Go backend: exact field matching, Pass/Fail only, no proof. It is the
// fallback that lets prova run with no prova-proof companion attached.
type Builtin struct {
	Expect map[string]string // field -> required value
}

// Judge returns Pass only if every expected field matches exactly.
func (b Builtin) Judge(_ context.Context, obs Observations) (Verdict, error) {
	for field, want := range b.Expect {
		if obs.Response[field] != want {
			return Verdict{Outcome: Fail}, nil
		}
	}
	return Verdict{Outcome: Pass}, nil
}
