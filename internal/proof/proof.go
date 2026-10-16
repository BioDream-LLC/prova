// Package proof is the bridge to the prova-proof companion: an external, independent prover that
// re-checks a verdict's argumentation framework and returns a certificate. If the companion binary
// is not attached, prova verifies the framework in-process so a verdict is always certified and the
// single binary remains self-sufficient.
//
// The companion speaks a tiny line protocol over stdin/stdout — deliberately trivial so a C++
// program needs no JSON library:
//
//	stdin : ARG <id>\n ... ATK <from> <to>\n ... QUERY <id>\n END\n
//	stdout: ENGINE <name>\n QUERY_GROUNDED <label>\n COMPLETE <0|1>\n
//	        STABLE_EXISTS <0|1>\n SKEPTICAL <0|1>\n CREDULOUS <0|1>\n OK\n
package proof

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/biodream-llc/prova/internal/af"
	"github.com/biodream-llc/prova/internal/verdict"
)

// Verify returns an independent certificate for the framework and query. It prefers the prova-proof
// companion (SAT-backed) and falls back to the in-process verifier; it never fails.
func Verify(ctx context.Context, fw af.Framework, query string) *verdict.Certificate {
	if p := companionPath(); p != "" {
		if c, err := runCompanion(ctx, p, fw, query); err == nil {
			return c
		}
	}
	return goVerify(fw, query)
}

// companionPath locates the prova-proof binary: explicit PROVA_PROOF, then alongside this
// executable (the shipped layout), then PATH. Empty means no companion attached.
func companionPath() string {
	if p := os.Getenv("PROVA_PROOF"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "prova-proof")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if p, err := exec.LookPath("prova-proof"); err == nil {
		return p
	}
	return ""
}

func encode(fw af.Framework, query string) []byte {
	var b strings.Builder
	for _, a := range fw.Args {
		fmt.Fprintf(&b, "ARG %s\n", a.ID)
	}
	for _, at := range fw.Attacks {
		fmt.Fprintf(&b, "ATK %s %s\n", at.From, at.To)
	}
	fmt.Fprintf(&b, "QUERY %s\nEND\n", query)
	return []byte(b.String())
}

func runCompanion(ctx context.Context, path string, fw af.Framework, query string) (*verdict.Certificate, error) {
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(encode(fw, query))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	cert := &verdict.Certificate{}
	ok := false
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "ENGINE":
			if len(fields) > 1 {
				cert.Engine = fields[1]
			}
		case "QUERY_GROUNDED":
			if len(fields) > 1 {
				cert.QueryLabel = fields[1]
			}
		case "COMPLETE":
			cert.Complete = oneField(fields)
		case "GROUNDED_VERIFIED":
			cert.GroundedVerified = oneField(fields)
		case "STABLE_EXISTS":
			cert.StableExists = oneField(fields)
		case "SKEPTICAL":
			cert.Skeptical = oneField(fields)
		case "CREDULOUS":
			cert.Credulous = oneField(fields)
		case "OK":
			ok = true
		}
	}
	if !ok || cert.Engine == "" {
		return nil, fmt.Errorf("proof: companion returned an incomplete certificate")
	}
	return cert, nil
}

func oneField(fields []string) bool { return len(fields) > 1 && fields[1] == "1" }

// goVerify computes the certificate in-process: grounded label of the query, completeness of the
// grounded labelling, and stable-semantics facts by enumeration.
func goVerify(fw af.Framework, query string) *verdict.Certificate {
	label := fw.Grounded()
	cert := &verdict.Certificate{
		Engine:           "builtin-go",
		QueryLabel:       string(label[query]),
		Complete:         fw.Complete(label),
		GroundedVerified: fw.GroundedVerified(query),
	}
	stable := fw.Stable()
	if stable == nil {
		return cert // framework too large for in-process enumeration
	}
	cert.StableExists = len(stable) > 0
	inAll, inSome := len(stable) > 0, false
	for _, s := range stable {
		if contains(s, query) {
			inSome = true
		} else {
			inAll = false
		}
	}
	cert.Skeptical = inAll
	cert.Credulous = inSome
	return cert
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
