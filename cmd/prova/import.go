package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/biodream-llc/prova/internal/capture"
	"github.com/biodream-llc/prova/internal/engine"
)

// importCLI is `prova import`: it turns captured traffic into a cassette `prova run --replay-live`
// replays against a target.
//
//	prova import --protocol fix --out orders.jsonl quickfix-messages.log
//	prova import --protocol iso8583 --dialect visa --mask --context availableBalance=142.00 --out auth.jsonl dump.hex
//	prova import --protocol iso20022 --out pay.jsonl payments.xml
func importCLI(args []string) int {
	fs := flag.NewFlagSet("prova import", flag.ContinueOnError)
	protocol := fs.String("protocol", "", "fix, iso8583 or iso20022")
	dialect := fs.String("dialect", "", "ISO 8583 dialect of the capture: generic, visa or mastercard")
	mask := fs.Bool("mask", false, "replace card numbers, IBANs, account ids and names with stable stand-ins")
	ctxFlag := fs.String("context", "", "seeded state for every recording, as key=value pairs separated by commas")
	out := fs.String("out", "", "write the cassette here (default: standard output)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *protocol == "" || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: prova import --protocol fix|iso8583|iso20022 [--dialect D] [--mask] [--context k=v,...] [--out cassette.jsonl] <capture | ->")
		return 2
	}
	opt := capture.Options{Dialect: *dialect, Mask: *mask, Context: map[string]string{}}
	for _, kv := range strings.Split(*ctxFlag, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
			opt.Context[k] = v
		}
	}
	var in io.Reader = os.Stdin
	if fs.Arg(0) != "-" {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova import:", err)
			return 1
		}
		defer f.Close()
		in = f
	}
	recs, rep, err := capture.Import(*protocol, in, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prova import:", err)
		return 1
	}
	path := *out
	if path == "" {
		path = "/dev/stdout"
	}
	if err := engine.WriteRecordings(path, recs); err != nil {
		fmt.Fprintln(os.Stderr, "prova import:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "prova import: %d messages read, %d requests kept, %d skipped %v", rep.Read, rep.Kept, rep.Skipped, rep.Kinds)
	if rep.Masked {
		fmt.Fprint(os.Stderr, ", masked")
	}
	fmt.Fprintln(os.Stderr)
	for _, n := range rep.Notes {
		fmt.Fprintln(os.Stderr, "  note:", n)
	}
	return 0
}
