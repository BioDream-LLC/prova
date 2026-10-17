package verdict

import (
	"strconv"
	"strings"

	"github.com/biodream-llc/prova/internal/af"
)

// Case is what the casebook judges: what the tester sent, what state the tester seeded (and
// therefore knows), and what the host actually returned.
type Case struct {
	Request  map[string]string `json:"request"`
	Context  map[string]string `json:"context"`
	Response map[string]string `json:"response"`
}

// Result is a decided, explainable verdict: the outcome, the framework it was decided over, the
// grounded labelling of every argument, and the cited chain that proves it. For Indeterminate,
// Settles lists the evidence that would decide it. Certificate records who independently re-checked
// the framework (the prova-proof companion when attached, else the in-process verifier).
type Result struct {
	Outcome     Outcome             `json:"outcome"`
	Framework   af.Framework        `json:"framework"`
	Labels      map[string]af.Label `json:"labels"`
	Chain       []af.Step           `json:"chain"`
	Settles     []string            `json:"settles,omitempty"`
	Certificate *Certificate        `json:"certificate,omitempty"`
	CrossCheck  *CrossCheck         `json:"crossCheck,omitempty"`
}

// CrossCheck is a second, independent verdict on the same case from a different engine (quorum's
// compiled casebook). Agrees is false when the two engines reached different outcomes — a finding
// worth investigating, because the rules were written twice, in two languages, by two routes.
type CrossCheck struct {
	Engine      string    `json:"engine"`
	Casebook    string    `json:"casebook"`
	Fingerprint string    `json:"fingerprint"`
	Outcome     Outcome   `json:"outcome"`
	Agrees      bool      `json:"agrees"`
	Readings    []string  `json:"readings"`
	Chain       []af.Step `json:"chain,omitempty"`
}

// Certificate is the independent re-check of a verdict's framework: the engine that produced it, the
// grounded label of the query, whether the labelling is a complete extension, and what stable
// semantics say about the query (existence, skeptical and credulous acceptance). The prova-proof
// companion derives skeptical/credulous via SAT; the in-process verifier derives them by search.
type Certificate struct {
	Engine           string `json:"engine"`
	QueryLabel       string `json:"queryLabel"`
	Complete         bool   `json:"complete"`
	GroundedVerified bool   `json:"groundedVerified"`
	StableExists     bool   `json:"stableExists"`
	Skeptical        bool   `json:"skeptical"`
	Credulous        bool   `json:"credulous"`
}

// passID is the sink claim: "the host behaved as the rule requires". A rule attacks it when the host
// misbehaves; the verdict is whatever grounded semantics make of pass.
const passID = "pass"

// builder accumulates the framework a casebook run produces.
type builder struct {
	fw    af.Framework
	have  map[string]bool
	needs []string
}

func newBuilder() *builder {
	b := &builder{have: map[string]bool{}}
	b.add(af.Arg{ID: passID, Claim: "the host behaved as the rule requires"})
	return b
}

func (b *builder) add(a af.Arg) {
	if !b.have[a.ID] {
		b.have[a.ID] = true
		b.fw.Args = append(b.fw.Args, a)
	}
}

func (b *builder) attack(from, to string, k af.Kind) {
	b.fw.Attacks = append(b.fw.Attacks, af.Attack{From: from, To: to, Kind: k})
}

// violate adds v and makes it attack the pass claim.
func (b *builder) violate(v af.Arg) {
	b.add(v)
	b.attack(v.ID, passID, af.Rebut)
}

func (b *builder) needsEvidence(key string) { b.needs = append(b.needs, key) }

// Rule contributes arguments and attacks for a Case.
type Rule struct {
	ID    string
	Title string
	Apply func(c Case, b *builder)
}

// Casebook is an ordered set of rules evaluated together against one Case.
type Casebook struct {
	Rules []Rule
}

// Default returns the shipped behavioural casebook.
func Default() Casebook {
	return Casebook{Rules: []Rule{
		RuleStructuralValidity(),
		RuleInsufficientFunds(),
		RuleCurrencyConsistency(),
		RuleValueDate(),
		RuleDuplicateInstruction(),
		RuleFieldValidity(),
		RuleAccountUsable(),
		RuleTransactionLimit(),
		RuleOriginalKnown(),
		RuleWithinOriginal(),
		RuleCancellation(),
		RuleStatusReport(),
		RuleMarketOpen(),
		RuleInstrumentListed(),
		RulePosition(),
		RuleExpectedReason(),
	}}
}

// RuleStructuralValidity is field-level message validation: the response must carry a recognized
// decision, and a rejection must carry a reason code. It is protocol-agnostic (canonical keys) and
// stays dormant for well-formed responses.
func RuleStructuralValidity() Rule {
	return Rule{
		ID:    "message.structural_validity",
		Title: "The response must be a well-formed, recognized status",
		Apply: func(c Case, b *builder) {
			decision := c.Response["decision"]
			switch decision {
			case "":
				b.violate(af.Arg{ID: "v_unrecognized", Claim: "response is not a recognized status", Cites: []string{"no decision field"}})
			case "rejected":
				if c.Response["rejectReason"] == "" {
					b.violate(af.Arg{ID: "v_noreason", Claim: "rejected without a reason code", Cites: []string{"missing reject reason"}})
				}
			}
		},
	}
}

// Prove evaluates every rule against the case, computes the grounded extension and returns the
// decided verdict with its proof.
func (cb Casebook) Prove(c Case) Result {
	b := newBuilder()
	for _, r := range cb.Rules {
		r.Apply(c, b)
	}
	label := b.fw.Grounded()
	if b.fw.Attacks == nil {
		b.fw.Attacks = []af.Attack{} // serialise as [], never null
	}
	var out Outcome
	switch label[passID] {
	case af.In:
		out = Pass
	case af.Out:
		out = Fail
	default:
		out = Indeterminate
	}
	res := Result{Outcome: out, Framework: b.fw, Labels: label, Chain: b.fw.Explain(passID)}
	if out == Indeterminate {
		res.Settles = b.needs
	}
	return res
}

// RuleInsufficientFunds encodes the core funds rule, protocol-agnostic: a payment/authorisation that
// exceeds the available balance must be rejected (RuleExpectedReason checks the reason code). It reads the canonical decision keys that every adapter emits
// (decision = accepted|rejected, rejectReason), so it judges ISO 20022 (AM04) and ISO 8583 (DE39=51)
// alike. It returns Indeterminate when the evidence to decide (seeded balance, overdraft) is unknown.
func RuleInsufficientFunds() Rule {
	return Rule{
		ID:    "funds.sufficiency",
		Title: "A payment exceeding available funds must be rejected",
		Apply: func(c Case, b *builder) {
			if !fundsApply(c) {
				return // no amount, or an instruction that does not debit the account (a return, a reversal)
			}
			amtStr := c.Request["Amount"]
			ccy := c.Request["Currency"]
			availStr := c.Context["availableBalance"]
			decision := c.Response["decision"]
			reason := c.Response["rejectReason"]

			b.add(af.Arg{ID: "ev_amt", Claim: "instructed amount " + amtStr + " " + ccy, Cites: []string{"request amount"}})

			if availStr == "" {
				u1 := af.Arg{ID: "maybe_short", Claim: "the payment may exceed the available balance"}
				u2 := af.Arg{ID: "maybe_ok", Claim: "the payment may be within the available balance"}
				b.add(u1)
				b.add(u2)
				b.attack(u1.ID, u2.ID, af.Rebut)
				b.attack(u2.ID, u1.ID, af.Rebut)
				b.attack(u1.ID, passID, af.Undercut)
				b.needsEvidence("context.availableBalance")
				return
			}

			b.add(af.Arg{ID: "ev_avail", Claim: "available balance " + availStr + " " + ccy, Cites: []string{"seeded ledger"}})
			amt := minor(amtStr)
			avail := minor(availStr)

			if amt > avail {
				short := af.Arg{ID: "shortfall", Claim: "amount exceeds available balance", Cites: []string{amtStr + " > " + availStr}}
				acc := af.Arg{ID: "accept_ok", Claim: "accepting the payment would be acceptable"}
				b.add(short)
				b.add(acc)
				b.attack(short.ID, acc.ID, af.Undermine)

				if decision != "rejected" {
					if c.Context["overdraftLimit"] == "unknown" {
						v := af.Arg{ID: "v_accept", Claim: "host accepted a payment exceeding available funds", Cites: []string{"response decision=" + decision}}
						b.violate(v)
						j := af.Arg{ID: "j_overdraft", Claim: "an overdraft facility may authorise it"}
						jq := af.Arg{ID: "j_unknown", Claim: "the overdraft limit is unknown"}
						b.add(j)
						b.add(jq)
						b.attack(j.ID, v.ID, af.Undermine)
						b.attack(j.ID, jq.ID, af.Rebut)
						b.attack(jq.ID, j.ID, af.Rebut)
						b.needsEvidence("context.overdraftLimit")
					} else {
						v := af.Arg{ID: "v_accept", Claim: "host accepted a payment exceeding available funds", Cites: []string{"response decision=" + decision}}
						b.violate(v)
					}
					return
				}
				rej := af.Arg{ID: "host_reject", Claim: "host rejected the payment", Cites: []string{"response decision=rejected"}}
				b.add(rej)
				b.attack(short.ID, rej.ID, af.Undercut) // the reason code is RuleExpectedReason's concern
				return
			}

			if decision == "rejected" && isFundsReason(reason) {
				v := af.Arg{ID: "v_wrongreject", Claim: "host rejected a payment the account could cover", Cites: []string{"response decision=rejected", amtStr + " <= " + availStr}}
				b.violate(v)
			}
		},
	}
}

// isFundsReason reports whether a reject reason is (or implies) insufficient funds. The funds rule
// only objects to a rejection when the host blamed funds; a rejection for another cause (currency,
// duplicate, value date) is that rule's concern, not this one — so the rules compose cleanly.
func isFundsReason(reason string) bool {
	switch reason {
	case "", "AM04", "51":
		return true
	default:
		return false
	}
}

// RuleCurrencyConsistency: when the scenario states the account's held currency, an instruction in a
// different currency is cross-currency and a compliant host must reject it (unless FX is explicitly
// allowed). It reads canonical keys, so it applies to any protocol that carries a currency. It stays
// dormant when the account currency is not seeded.
func RuleCurrencyConsistency() Rule {
	return Rule{
		ID:    "currency.consistency",
		Title: "An instruction in a currency the account does not hold must be rejected",
		Apply: func(c Case, b *builder) {
			acctCcy := strings.ToUpper(strings.TrimSpace(c.Context["accountCurrency"]))
			reqCcy := strings.ToUpper(strings.TrimSpace(c.Request["Currency"]))
			if acctCcy == "" || reqCcy == "" || acctCcy == reqCcy {
				return
			}
			if c.Context["fxAllowed"] == "true" {
				return
			}
			b.add(af.Arg{ID: "ev_ccy", Claim: "instructed " + reqCcy + " on a " + acctCcy + " account", Cites: []string{"accountCurrency " + acctCcy}})
			if c.Response["decision"] == "accepted" {
				b.violate(af.Arg{ID: "v_ccy", Claim: "host accepted a cross-currency instruction with no FX agreed", Cites: []string{reqCcy + " != " + acctCcy}})
			}
		},
	}
}

// RuleValueDate: a back-valued instruction (settlement value date earlier than the processing date)
// must be rejected unless back-valuing is permitted. Dates are compared as ISO YYYY-MM-DD strings.
// Dormant unless both the value date and the settlement/processing date are seeded.
func RuleValueDate() Rule {
	return Rule{
		ID:    "valuedate.not_back_valued",
		Title: "A back-valued instruction must be rejected",
		Apply: func(c Case, b *builder) {
			vd := strings.TrimSpace(c.Context["valueDate"])
			sd := strings.TrimSpace(c.Context["settlementDate"])
			if vd == "" || sd == "" || vd >= sd {
				return
			}
			if c.Context["backValueAllowed"] == "true" {
				return
			}
			b.add(af.Arg{ID: "ev_vdate", Claim: "value date " + vd + " precedes settlement date " + sd, Cites: []string{"back-valued"}})
			if c.Response["decision"] == "accepted" {
				b.violate(af.Arg{ID: "v_vdate", Claim: "host accepted a back-valued instruction", Cites: []string{vd + " < " + sd}})
			}
		},
	}
}

// RuleDuplicateInstruction: when the scenario states this instruction was already processed, the
// host must reject the resubmission; accepting it risks a double payment. Dormant unless the
// scenario seeds duplicate=true.
func RuleDuplicateInstruction() Rule {
	return Rule{
		ID:    "idempotency.duplicate",
		Title: "A duplicate instruction must be rejected, not processed twice",
		Apply: func(c Case, b *builder) {
			if c.Context["duplicate"] != "true" {
				return
			}
			id := firstNonEmpty(c.Request["EndToEndId"], c.Request["STAN"], c.Request["ClOrdID"], "the instruction")
			b.add(af.Arg{ID: "ev_dup", Claim: id + " was already processed", Cites: []string{"seeded duplicate"}})
			if c.Response["decision"] == "accepted" {
				b.violate(af.Arg{ID: "v_dup", Claim: "host processed a duplicate instruction (double-payment risk)", Cites: []string{"duplicate " + id}})
			}
		},
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// minor parses "150.00" into integer minor units (15000), tolerant of sign and short fractions.
func minor(s string) int64 {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt(frac, 10, 64)
	v := w*100 + f
	if neg {
		v = -v
	}
	return v
}
