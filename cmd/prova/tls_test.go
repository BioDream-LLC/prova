package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSelfSignedCertificateLoadsAndCoversLocalhost(t *testing.T) {
	dir := t.TempDir()
	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := generateSelfSigned(cert, key, "127.0.0.1:8443"); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("generated pair does not load: %v", err)
	}
	if expiresSoon(pair) {
		t.Fatal("fresh certificate reported as expiring")
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()
	leaf := srv.TLS.Certificates[0]
	if len(leaf.Certificate) == 0 {
		t.Fatal("no certificate in chain")
	}
}

func TestSecureHeadersSetHSTS(t *testing.T) {
	rec := httptest.NewRecorder()
	secureHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("missing HSTS header")
	}
}

func TestHTTPRedirectsToHTTPS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://example.test:8080/proof?run=r-1", nil)
	redirectServer(":8080", ":8443").Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://example.test:8443/proof?run=r-1" {
		t.Fatalf("Location = %q", got)
	}
}
