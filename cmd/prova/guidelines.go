package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/biodream-llc/prova/internal/validate"
)

// registerGuidelines serves the usage-guideline library: everyone may list and download; an admin
// imports a set (an XSD or a ZIP, such as a MyStandards or EPC download) or removes one.
func registerGuidelines(mux *http.ServeMux, lib *validate.Library, activity func(*http.Request, string, string)) {
	mux.HandleFunc("GET /api/guidelines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, lib.List())
	})
	mux.HandleFunc("GET /api/guidelines/{set}/{file}", func(w http.ResponseWriter, r *http.Request) {
		raw, err := lib.Schema(r.PathValue("set") + "/" + r.PathValue("file"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("file")+`.xsd"`)
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("POST /api/guidelines", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Set       string `json:"set"`
			Name      string `json:"name"`
			Publisher string `json:"publisher"`
			FileName  string `json:"fileName"`
			Data      string `json:"data"` // base64
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 48<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		data, err := base64.StdEncoding.DecodeString(body.Data)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data is not base64"})
			return
		}
		rep, err := lib.Import(body.Set, body.Name, body.Publisher, body.FileName, data)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "skipped": rep.Skipped})
			return
		}
		activity(r, "imported usage guidelines", fmt.Sprintf("%s: %d schemas from %s (%d files skipped)", body.Set, len(rep.Added), body.FileName, len(rep.Skipped)))
		writeJSON(w, http.StatusOK, rep)
	})
	mux.HandleFunc("DELETE /api/guidelines/{set}", func(w http.ResponseWriter, r *http.Request) {
		if err := lib.Remove(r.PathValue("set")); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		activity(r, "removed usage guidelines", r.PathValue("set"))
		w.WriteHeader(http.StatusNoContent)
	})
}

const guidelineUsage = `usage:
  prova guideline [-data DIR] list [KIND]
  prova guideline [-data DIR] add SET FILE.xsd|FILE.zip [-name NAME] [-publisher WHO]
  prova guideline [-data DIR] remove SET
  prova guideline [-data DIR] schema ID
SET names the collection (letters, digits, dot, dash, underscore). ID is SET/FILE, as list shows.`

// guidelineCLI is `prova guideline`.
func guidelineCLI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prova guideline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data", defaultDataDir(), "data directory")
	name := fs.String("name", "", "the set's name, for add")
	publisher := fs.String("publisher", "", "who publishes the guideline, for add")
	fs.Usage = func() { fmt.Fprintln(stderr, guidelineUsage); fs.PrintDefaults() }
	// flags may come before or after the command
	var pos []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(pos) == 0 {
		fs.Usage()
		return 2
	}
	lib := validate.OpenLibrary(*dataDir)
	switch pos[0] {
	case "list":
		for _, g := range lib.List() {
			if len(pos) > 1 && g.Kind != pos[1] {
				continue
			}
			origin := "imported"
			if g.Bundled {
				origin = "bundled"
			}
			fmt.Fprintf(stdout, "%-46s %-16s %-9s %s\n", g.ID, g.Kind, origin, g.SetName)
		}
		return 0
	case "add":
		if len(pos) != 3 {
			fs.Usage()
			return 2
		}
		data, err := os.ReadFile(pos[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		rep, err := lib.Import(pos[1], *name, *publisher, filepath.Base(pos[2]), data)
		for _, s := range rep.Skipped {
			fmt.Fprintf(stderr, "skipped %s: %s\n", s.File, s.Reason)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, g := range rep.Added {
			fmt.Fprintf(stdout, "added %s (%s)\n", g.ID, g.Kind)
		}
		return 0
	case "remove":
		if len(pos) != 2 {
			fs.Usage()
			return 2
		}
		if err := lib.Remove(pos[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "removed "+pos[1])
		return 0
	case "schema":
		if len(pos) != 2 {
			fs.Usage()
			return 2
		}
		raw, err := lib.Schema(strings.TrimSuffix(pos[1], ".xsd"))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_, _ = stdout.Write(raw)
		return 0
	}
	fs.Usage()
	return 2
}
