package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biodream-llc/prova/internal/engine"
)

// TestCatalogueAPI lists the catalogue, fetches a sample, a broken variant and a schema, and sends a
// catalogue message to the compliant and the defective gateways.
func TestCatalogueAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := engine.New()
	startSims(ctx, eng, false)
	mux := http.NewServeMux()
	registerCatalogue(mux, eng, func(r *http.Request) context.Context { return r.Context() }, func(*http.Request, string, string) {})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func(path string) (int, []byte) {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(res.Body)
		return res.StatusCode, b.Bytes()
	}
	code, body := get("/api/catalogue")
	var cat struct {
		Messages []struct{ Kind string } `json:"messages"`
	}
	if code != 200 || json.Unmarshal(body, &cat) != nil || len(cat.Messages) != 2641 {
		t.Fatalf("catalogue: %d, %d messages", code, len(cat.Messages))
	}
	if code, body = get("/api/catalogue/sese.023/sample"); code != 200 || !strings.Contains(string(body), "SctiesSttlmTxInstr") {
		t.Fatalf("sample: %d %s", code, body)
	}
	if code, _ = get("/api/catalogue/sese.023/sample?variant=nonsense"); code != 400 {
		t.Errorf("unknown variant: %d", code)
	}
	if code, body = get("/api/catalogue/camt.053.001.02/schema"); code != 200 || !strings.Contains(string(body), "camt.053.001.02") {
		t.Errorf("schema: %d", code)
	}
	if code, _ = get("/api/catalogue/zzzz.999/sample"); code != 404 {
		t.Errorf("unknown message: %d", code)
	}
	for _, c := range []struct{ variant, target, outcome, reply string }{
		{"minimal", "cbpr-uat", "pass", "admi.007"},
		{"drop", "cbpr-uat", "pass", "admi.002"},
		{"drop", "switch-sim", "fail", "admi.007"},
	} {
		res, err := http.Post(srv.URL+"/api/catalogue/run", "application/json",
			strings.NewReader(`{"kind":"seev.031","variant":"`+c.variant+`","target":"`+c.target+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var run engine.Run
		_ = json.NewDecoder(res.Body).Decode(&run)
		res.Body.Close()
		if string(run.Outcome) != c.outcome || run.Messages[1].Kind != c.reply {
			t.Errorf("%s@%s: %s with %s, want %s with %s", c.variant, c.target, run.Outcome, run.Messages[1].Kind, c.outcome, c.reply)
		}
	}
}

func TestCatalogueCLI(t *testing.T) {
	var out, errb bytes.Buffer
	if code := catalogueCLI([]string{"list", "fxtr"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "fxtr.014") {
		t.Fatalf("list: %d %s %s", code, out.String(), errb.String())
	}
	out.Reset()
	if code := catalogueCLI([]string{"sample", "camt.053", "drop"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "BkToCstmrStmt") {
		t.Fatalf("sample: %d %s", code, errb.String())
	}
	if code := catalogueCLI([]string{"sample", "zzzz.001"}, &out, &errb); code != 1 {
		t.Errorf("unknown message: %d", code)
	}
}
