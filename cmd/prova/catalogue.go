package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/biodream-llc/prova/internal/engine"
	"github.com/biodream-llc/prova/internal/validate"
)

// registerCatalogue serves the ISO 20022 catalogue: every message version, a generated sample or
// broken variant of any of them, its official schema, and a run that sends one to a target.
func registerCatalogue(mux *http.ServeMux, eng *engine.Engine, acting func(*http.Request) context.Context, activity func(*http.Request, string, string)) {
	mux.HandleFunc("GET /api/catalogue", func(w http.ResponseWriter, r *http.Request) {
		variants := make([]map[string]string, 0, len(validate.VariantNames))
		for _, v := range validate.VariantNames {
			variants = append(variants, map[string]string{"id": v, "description": validate.VariantDescriptions[v]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"areas": validate.Areas, "messages": validate.Catalogue(), "variants": variants})
	})
	mux.HandleFunc("GET /api/catalogue/{kind}/sample", func(w http.ResponseWriter, r *http.Request) {
		m, ok := validate.LookupMessage(r.PathValue("kind"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such message in the ISO 20022 catalogue"})
			return
		}
		v := r.URL.Query().Get("variant")
		if v == "" {
			v = "minimal"
		}
		if _, known := validate.VariantDescriptions[v]; !known {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown variant " + v})
			return
		}
		raw, err := validate.Variant(m.Kind, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("GET /api/catalogue/{kind}/schema", func(w http.ResponseWriter, r *http.Request) {
		m, ok := validate.LookupMessage(r.PathValue("kind"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such message in the ISO 20022 catalogue"})
			return
		}
		raw, err := validate.CatalogueSchema(m.Kind)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+m.Kind+`.xsd"`)
		_, _ = w.Write(raw)
	})
	// Send one catalogue message (a generated variant, or the caller's own XML) to an ISO 20022 target.
	mux.HandleFunc("POST /api/catalogue/run", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind    string `json:"kind"`
			Variant string `json:"variant"`
			XML     string `json:"xml"`
			Target  string `json:"target"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		m, ok := validate.LookupMessage(body.Kind)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such message in the ISO 20022 catalogue"})
			return
		}
		if body.Variant == "" {
			body.Variant = "minimal"
		}
		req := map[string]string{"Variant": body.Variant}
		what := validate.VariantDescriptions[body.Variant]
		if strings.TrimSpace(body.XML) != "" {
			req = map[string]string{"XML": body.XML}
			what = "the message as given"
		}
		sc := engine.Scenario{ID: "catalogue-" + m.Kind, Name: m.Base + " " + m.Name + " — " + what, Protocol: "iso20022", Message: m.Kind,
			Description: "Send " + m.Kind + " (" + what + ") to a gateway: a schema-valid message must be acknowledged, one that fails its schema rejected.",
			Request:     req, Context: map[string]string{}}
		run, err := eng.RunScenario(acting(r), sc, body.Target)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		activity(r, "sent a catalogue message", fmt.Sprintf("%s (%s) on %s: %s (%s)", m.Kind, body.Variant, run.Target, run.Outcome, run.ID))
		writeJSON(w, http.StatusOK, run)
	})
}
