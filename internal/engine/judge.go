package engine

import (
	"github.com/biodream-llc/prova/internal/qcasebook"
	"github.com/biodream-llc/prova/internal/verdict"
)

// judge decides a case with the primary (Go) casebook and cross-checks it with the second,
// independent engine — quorum's compiled casebook. Both verdicts travel with the result, so a
// disagreement is visible on the Proof screen, in the API and in reports.
func judge(book verdict.Casebook, c verdict.Case) verdict.Result {
	res := book.Prove(c)
	res.CrossCheck = qcasebook.Default().CrossCheck(c, res.Outcome)
	return res
}
