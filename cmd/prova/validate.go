package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/biodream-llc/prova/internal/validate"
)

// validateCLI is `prova validate`: it validates an ISO 20022 message against the bundled schema and,
// optionally, a usage-guideline profile, and prints the findings (text or SARIF). It exits non-zero
// when the message has an error-severity finding, so it works as a CI gate.
func validateCLI(args []string) int {
	fs := flag.NewFlagSet("prova validate", flag.ContinueOnError)
	profile := fs.String("profile", "", "usage-guideline profile to apply on top of the schema (e.g. CBPR+)")
	sarif := fs.Bool("sarif", false, "write findings as SARIF 2.1.0 instead of text")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: prova validate [--profile CBPR+] [--sarif] <message.xml>")
		fmt.Fprintln(os.Stderr, "       prova validate [--profile CBPR+] -   # read the message from stdin")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	var raw []byte
	var err error
	if fs.Arg(0) == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "prova validate: %v\n", err)
		return 2
	}

	rep, err := validate.Message(raw, *profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prova validate: %v\n", err)
		return 2
	}

	if *sarif {
		sb, err := rep.SARIF()
		if err != nil {
			fmt.Fprintf(os.Stderr, "prova validate: %v\n", err)
			return 2
		}
		fmt.Println(string(sb))
		if rep.Valid() {
			return 0
		}
		return 1
	}

	fmt.Printf("%s  schema=%v  profile=%s\n", orUnknown(rep.Kind), rep.Schema, orUnknown(rep.Profile))
	if len(rep.Findings) == 0 {
		fmt.Println("no findings — the message is valid")
		return 0
	}
	errs := 0
	for _, f := range rep.Findings {
		fmt.Printf("  [%s] %s  %s\n          %s\n", f.Severity, f.Rule, f.Path, f.Message)
		if f.Fix != "" {
			fmt.Printf("          fix: %s\n", f.Fix)
		}
		if f.Severity == validate.Error {
			errs++
		}
	}
	fmt.Printf("%d findings (%d errors)\n", len(rep.Findings), errs)
	if errs > 0 {
		return 1
	}
	return 0
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
