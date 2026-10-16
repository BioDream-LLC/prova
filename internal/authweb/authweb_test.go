package authweb

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/auth"
	"github.com/biodream-llc/prova/internal/oidc"
	"github.com/biodream-llc/prova/internal/store"
)

func init() { auth.Iterations = 1000 }

func testUsers(t *testing.T) *auth.Users {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "prova.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return auth.NewUsers(st)
}

// app is the guarded application: one read endpoint and one write endpoint behind the sign-in layer.
func app(t *testing.T, o *OIDC) (*Server, http.Handler, *auth.Users) {
	t.Helper()
	users := testUsers(t)
	s := New(auth.NewSessions(users), o, nil, true)
	mux := http.NewServeMux()
	s.Register(mux)
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("[]")) })
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("{}")) })
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>")) })
	return s, s.Guard(mux), users
}

func do(h http.Handler, method, path, body string, cookie *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://prova.test"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func login(t *testing.T, h http.Handler, name, pw string) *http.Cookie {
	t.Helper()
	w := do(h, "POST", "/api/auth/login", `{"name":"`+name+`","password":"`+pw+`"}`, nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", name, w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("session cookie not hardened: %+v", c)
			}
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestGuard(t *testing.T) {
	_, h, users := app(t, nil)
	viewerPW, _ := users.Set("vic", auth.Viewer)
	opPW, _ := users.Set("ops", auth.Operator)
	adminPW, _ := users.Set("root", auth.Admin)

	if w := do(h, "GET", "/api/runs", "", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API read: %d", w.Code)
	}
	// Signed out, "who am I" is an empty answer, not an error a browser would log.
	if w := do(h, "GET", "/api/auth/me", "", nil, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"user"`) {
		t.Fatalf("signed-out me: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/", "/assets/x.js", "/api/health", "/api/auth/providers"} {
		if w := do(h, "GET", p, "", nil, nil); w.Code == http.StatusUnauthorized {
			t.Fatalf("%s should be public", p)
		}
	}
	if w := do(h, "POST", "/api/auth/login", `{"name":"vic","password":"nope"}`, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: %d", w.Code)
	}

	vic := login(t, h, "vic", viewerPW)
	if w := do(h, "GET", "/api/runs", "", vic, nil); w.Code != http.StatusOK {
		t.Fatalf("viewer read: %d", w.Code)
	}
	if w := do(h, "POST", "/api/runs", `{}`, vic, nil); w.Code != http.StatusForbidden {
		t.Fatalf("viewer write must be refused: %d", w.Code)
	}
	ops := login(t, h, "ops", opPW)
	if w := do(h, "POST", "/api/runs", `{}`, ops, nil); w.Code != http.StatusOK {
		t.Fatalf("operator write: %d", w.Code)
	}
	if w := do(h, "GET", "/api/users", "", ops, nil); w.Code != http.StatusForbidden {
		t.Fatalf("operator must not manage accounts: %d", w.Code)
	}

	// Cross-site writes are refused even with a valid session.
	if w := do(h, "POST", "/api/runs", `{}`, ops, map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin write: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "https://prova.test/api/runs", strings.NewReader("a=b"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(ops)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("form-encoded write: %d", w.Code)
	}

	root := login(t, h, "root", adminPW)
	if w := do(h, "POST", "/api/users", `{"name":"newbie","role":"viewer"}`, root, nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"password"`) {
		t.Fatalf("admin create user: %d %s", w.Code, w.Body)
	}
	if w := do(h, "DELETE", "/api/users/root", "", root, map[string]string{"Content-Type": "application/json"}); w.Code != http.StatusBadRequest {
		t.Fatalf("self-removal must be refused: %d", w.Code)
	}

	if w := do(h, "POST", "/api/auth/logout", `{}`, ops, nil); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := do(h, "GET", "/api/runs", "", ops, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("session must end at logout: %d", w.Code)
	}
}

// TestOIDCSignIn drives the whole authorization-code flow against a fake identity provider.
func TestOIDCSignIn(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	b64 := base64.RawURLEncoding.EncodeToString
	var nonce string
	mux := http.NewServeMux()
	idp := httptest.NewServer(mux)
	defer idp.Close()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"issuer": idp.URL, "authorization_endpoint": idp.URL + "/auth",
			"token_endpoint": idp.URL + "/token", "jwks_uri": idp.URL + "/keys"})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "k1", "use": "sig",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes())}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
		c, _ := json.Marshal(map[string]any{"iss": idp.URL, "aud": "prova", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": nonce, "preferred_username": "Dana@corp.example"})
		signed := b64(h) + "." + b64(c)
		sum := sha256.Sum256([]byte(signed))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		json.NewEncoder(w).Encode(map[string]string{"id_token": signed + "." + b64(sig)})
	})

	prov, err := oidc.Discover(context.Background(), oidc.Config{Issuer: idp.URL, ClientID: "prova", ClientSecret: "s",
		RedirectURL: "https://prova.test/api/auth/oidc/callback"})
	if err != nil {
		t.Fatal(err)
	}
	_, h, users := app(t, &OIDC{Provider: prov})
	if err := users.SetSSO("dana", auth.Operator); err != nil {
		t.Fatal(err)
	}

	start := do(h, "GET", "/api/auth/oidc/start", "", nil, nil)
	if start.Code != http.StatusFound {
		t.Fatalf("start: %d", start.Code)
	}
	loc, _ := url.Parse(start.Header().Get("Location"))
	state, nonce := loc.Query().Get("state"), loc.Query().Get("nonce")
	var stateCookie *http.Cookie
	for _, c := range start.Result().Cookies() {
		if c.Name == oidcCookie {
			stateCookie = c
		}
	}
	if stateCookie == nil || state == "" || nonce == "" {
		t.Fatal("start did not set state/nonce")
	}

	// A callback without the browser's state cookie is refused.
	if w := do(h, "GET", "/api/auth/oidc/callback?code=good-code&state="+state, "", nil, nil); !strings.Contains(w.Header().Get("Location"), "sso=failed") {
		t.Fatalf("callback without state cookie should fail, went to %s", w.Header().Get("Location"))
	}
	// Restart (the state is single-use) and complete properly.
	start = do(h, "GET", "/api/auth/oidc/start", "", nil, nil)
	loc, _ = url.Parse(start.Header().Get("Location"))
	state, nonce = loc.Query().Get("state"), loc.Query().Get("nonce")
	for _, c := range start.Result().Cookies() {
		if c.Name == oidcCookie {
			stateCookie = c
		}
	}
	cb := do(h, "GET", "/api/auth/oidc/callback?code=good-code&state="+state, "", stateCookie, nil)
	if cb.Header().Get("Location") != "/" {
		t.Fatalf("callback went to %q", cb.Header().Get("Location"))
	}
	var sess *http.Cookie
	for _, c := range cb.Result().Cookies() {
		if c.Name == CookieName {
			sess = c
		}
	}
	if sess == nil {
		t.Fatal("no session after OIDC sign-in")
	}
	me := do(h, "GET", "/api/auth/me", "", sess, nil)
	if !strings.Contains(me.Body.String(), `"user":"dana"`) || !strings.Contains(me.Body.String(), `"via":"oidc"`) {
		t.Fatalf("me: %s", me.Body)
	}
}
