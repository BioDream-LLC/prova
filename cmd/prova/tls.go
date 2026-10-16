package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// prova serves its interface and API over HTTPS only. With -tls-cert/-tls-key it uses the operator's
// certificate (the right choice for any shared deployment). Without them it uses a self-signed
// certificate it generates once and keeps in the user config directory, so the browser's trust
// decision survives restarts and the fingerprint printed at startup stays stable.

// tlsConfig returns the server TLS configuration and a one-line note describing the certificate.
func tlsConfig(certFile, keyFile, addr string) (*tls.Config, string, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, "", errors.New("-tls-cert and -tls-key must be given together")
		}
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, "", fmt.Errorf("load certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
		return cfg, "TLS certificate " + certFile + " (" + fingerprintOf(pair) + ")", nil
	}

	dir, err := tlsDir()
	if err != nil {
		return nil, "", err
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil || expiresSoon(pair) {
		if err := generateSelfSigned(certPath, keyPath, addr); err != nil {
			return nil, "", fmt.Errorf("generate self-signed certificate: %w", err)
		}
		if pair, err = tls.LoadX509KeyPair(certPath, keyPath); err != nil {
			return nil, "", err
		}
	}
	cfg.Certificates = []tls.Certificate{pair}
	note := "self-signed TLS certificate " + certPath + " (SHA-256 " + fingerprintOf(pair) +
		"); browsers will ask you to trust it once. Use -tls-cert/-tls-key for a CA-issued certificate."
	return cfg, note, nil
}

func tlsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "prova", "tls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func expiresSoon(pair tls.Certificate) bool {
	if len(pair.Certificate) == 0 {
		return true
	}
	c, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return true
	}
	return time.Until(c.NotAfter) < 30*24*time.Hour
}

func fingerprintOf(pair tls.Certificate) string {
	if len(pair.Certificate) == 0 {
		return "?"
	}
	sum := sha256.Sum256(pair.Certificate[0])
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	var parts []string
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts[:8], ":") + "…"
}

// generateSelfSigned writes an ECDSA P-256 certificate valid for localhost, this host's name and
// every address on its interfaces, plus the host part of addr if one was given.
func generateSelfSigned(certPath, keyPath, addr string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"prova (self-signed)"}, CommonName: "prova"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, h)
		if !strings.Contains(h, ".") {
			tmpl.DNSNames = append(tmpl.DNSNames, h+".local")
		}
	}
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ipn.IP)
			}
		}
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if ip == nil {
			tmpl.DNSNames = append(tmpl.DNSNames, host)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// secureHeaders adds HSTS and conservative browser hardening to every response.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// redirectServer answers plain HTTP on listen with a permanent redirect to the HTTPS address.
func redirectServer(listen, httpsAddr string) *http.Server {
	_, httpsPort, _ := net.SplitHostPort(httpsAddr)
	return &http.Server{
		Addr:              listen,
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			target := "https://" + host
			if httpsPort != "" && httpsPort != "443" {
				target += ":" + httpsPort
			}
			http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusPermanentRedirect)
		}),
	}
}

// displayAddr renders a listen address as something a browser can open.
func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	if host, port, err := net.SplitHostPort(addr); err == nil && (host == "0.0.0.0" || host == "::") {
		return "localhost:" + port
	}
	return addr
}
