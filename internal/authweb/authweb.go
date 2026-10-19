// Package authweb is prova's sign-in layer over HTTP: the guard every request passes, the session cookie, and the three
// ways in — a local account's password, an OpenID Connect provider, and a SAML 2.0 identity provider. Any combination
// may be enabled at once; the sign-in screen offers whatever is configured. It follows vaglio's server (BioDream LLC).
//
// The guard:
//   - every /api/ path needs a session except the sign-in endpoints, /api/health and /api/version;
//   - reading needs the viewer role, changing anything the operator role, and managing accounts the admin role;
//   - a state-changing request must be JSON from this origin, so no other site can make a signed-in browser act
//     (the SAML provider's form POST is the one exception: its signature and InResponseTo check stand in for it);
//   - the interface's own files are public, since they hold no data: the interface shows the sign-in screen until
//     /api/auth/me says who you are.
package authweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/biodream-llc/prova/internal/auth"
	"github.com/biodream-llc/prova/internal/oidc"
	"github.com/biodream-llc/prova/internal/saml"
	"github.com/biodream-llc/prova/internal/store"
)

// CookieName is the session cookie.
const CookieName = "prova_session"

const (
	oidcCookie   = "prova_oidc"
	signInWindow = 10 * time.Minute
	maxBody      = 1 << 20
)

// OIDC is single sign-on through an OpenID Connect provider.
type OIDC struct {
	Provider *oidc.Provider
	Label    string
}

// SAML is single sign-on through a SAML 2.0 identity provider (Microsoft Entra ID, Okta, ADFS, Keycloak).
type SAML struct {
	SP *saml.ServiceProvider
	// EntityID and ACSURL describe this service provider, for the metadata an identity provider administrator imports.
	EntityID, ACSURL string
	// NameAttribute names the attribute holding the account name. Empty: email, then a user principal name, then NameID.
	NameAttribute string
	// Group, when set, is a group the person must be in, read from GroupsAttribute (default "groups").
	Group, GroupsAttribute string
	Label                  string
}

// Demo is a published demonstration account: the sign-in screen shows its name and password. It is always a viewer.
type Demo struct {
	Name, Password string
}

// Server holds the sign-in configuration.
type Server struct {
	Sessions *auth.Sessions
	OIDC     *OIDC
	SAML     *SAML
	Secure   bool // served over TLS: cookies are marked Secure
	Demo     *Demo

	// Activity, when set, records sign-ins, sign-outs and account changes in the activity log.
	Activity func(actor, action, detail string)

	oidcPending *pending
	samlPending *pending
}

// New returns a sign-in server over the sessions.
func New(sessions *auth.Sessions, o *OIDC, s *SAML, secure bool) *Server {
	if o != nil && o.Label == "" {
		o.Label = "Sign in with your organization (OpenID Connect)"
	}
	if s != nil {
		if s.Label == "" {
			s.Label = "Sign in with your organization (SAML)"
		}
		if s.GroupsAttribute == "" {
			s.GroupsAttribute = "groups"
		}
	}
	return &Server{Sessions: sessions, OIDC: o, SAML: s, Secure: secure,
		oidcPending: &pending{m: map[string]pendingSignIn{}}, samlPending: &pending{m: map[string]pendingSignIn{}}}
}

// Register adds the sign-in endpoints to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/providers", s.providers)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/auth/oidc/start", s.oidcStart)
	mux.HandleFunc("GET /api/auth/oidc/callback", s.oidcCallback)
	mux.HandleFunc("GET /api/auth/saml/start", s.samlStart)
	mux.HandleFunc("POST /api/auth/saml/acs", s.samlACS)
	mux.HandleFunc("GET /api/auth/saml/metadata", s.samlMetadata)

	mux.HandleFunc("GET /api/users", s.listUsers)
	mux.HandleFunc("POST /api/users", s.setUser)
	mux.HandleFunc("DELETE /api/users/{name}", s.removeUser)
	mux.HandleFunc("GET /api/activity", s.activity)
}

func (s *Server) record(actor, action, detail string) {
	if s.Activity != nil {
		s.Activity(actor, action, detail)
	}
}

// public is whether a path needs no session.
func public(path string) bool {
	switch path {
	case "/api/health", "/api/version", "/api/auth/providers", "/api/auth/login", "/api/auth/logout", "/api/auth/me",
		"/api/auth/oidc/start", "/api/auth/oidc/callback", "/api/auth/saml/start", "/api/auth/saml/acs",
		"/api/auth/saml/metadata":
		return true
	}
	return !strings.HasPrefix(path, "/api/")
}

// need is the role a request needs.
func need(r *http.Request) auth.Role {
	if strings.HasPrefix(r.URL.Path, "/api/users") || r.URL.Path == "/api/activity" {
		return auth.Admin
	}
	// /api/validate and /api/counterparty are POSTs but change nothing — they compute from the body
	// and return a result — so a viewer (including the public demonstration) may use them.
	if r.URL.Path == "/api/validate" || r.URL.Path == "/api/counterparty" {
		return auth.Viewer
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return auth.Viewer
	}
	return auth.Operator
}

type ctxKey struct{}

// SessionFrom returns the session a guarded request carries.
func SessionFrom(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(auth.Session)
	return s, ok
}

// Guard wraps the whole application: it enforces sessions, roles and same-origin writes.
func (s *Server) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := strings.HasPrefix(r.URL.Path, "/api/")
		if api {
			w.Header().Set("Cache-Control", "no-store")
		}
		if api && r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/auth/saml/acs" {
			if msg := sameOrigin(r); msg != "" {
				writeError(w, http.StatusForbidden, msg)
				return
			}
		}
		if public(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sess, ok := s.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "sign in first")
			return
		}
		if n := need(r); !sess.Role.Can(n) {
			writeError(w, http.StatusForbidden, "this needs the "+string(n)+" role")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

func (s *Server) session(r *http.Request) (auth.Session, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return auth.Session{}, false
	}
	return s.Sessions.Get(c.Value)
}

// sameOrigin refuses a state-changing request another site could have made: it must be JSON (which a form cannot send,
// and a cross-site script cannot without the preflight this server never grants), and an Origin, when the browser sends
// one, must be this server.
func sameOrigin(r *http.Request) string {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return "a request that changes anything must be application/json"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			return "cross-origin request refused"
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return "cross-origin request refused"
	}
	return ""
}

func (s *Server) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteStrictMode})
}

// --- local accounts --------------------------------------------------------------------------------------------------

// Me is the signed-in user. Signed out, it is empty: /api/auth/me answers 200 either way, because a browser reports
// every 401 in its console and "not signed in yet" is not an error.
type Me struct {
	User string    `json:"user,omitempty"`
	Role auth.Role `json:"role,omitempty"`
	Via  string    `json:"via,omitempty"`
	Demo bool      `json:"demo,omitempty"` // this server is a public demonstration
}

func (s *Server) providers(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{"local": true}
	if s.OIDC != nil {
		out["oidc"] = map[string]string{"label": s.OIDC.Label, "start": "/api/auth/oidc/start"}
	}
	if s.SAML != nil {
		out["saml"] = map[string]string{"label": s.SAML.Label, "start": "/api/auth/saml/start"}
	}
	if s.Demo != nil {
		out["demo"] = map[string]string{"name": s.Demo.Name, "password": s.Demo.Password}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(body.Name))
	token, sess, err := s.Sessions.SignIn(name, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrBadCredentials) || errors.Is(err, auth.ErrLocked) {
			log.Printf("auth: sign-in refused for %q from %s: %v", name, r.RemoteAddr, err)
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("auth: %s signed in (password) from %s", sess.User, r.RemoteAddr)
	s.record(sess.User, "signed in", "password")
	s.setSession(w, token)
	writeJSON(w, http.StatusOK, Me{User: sess.User, Role: sess.Role, Via: sess.Via, Demo: s.Demo != nil})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		if sess, ok := s.Sessions.Get(c.Value); ok {
			log.Printf("auth: %s signed out", sess.User)
			s.record(sess.User, "signed out", "")
		}
		s.Sessions.SignOut(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		writeJSON(w, http.StatusOK, Me{Demo: s.Demo != nil})
		return
	}
	writeJSON(w, http.StatusOK, Me{User: sess.User, Role: sess.Role, Via: sess.Via, Demo: s.Demo != nil})
}

// --- account management (admin) --------------------------------------------------------------------------------------

func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	us, err := s.Sessions.Users().List()
	if us == nil {
		us = []auth.User{}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, us)
}

func (s *Server) setUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string    `json:"name"`
		Role auth.Role `json:"role"`
		SSO  bool      `json:"sso"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	actor, _ := SessionFrom(r.Context())
	name := strings.ToLower(strings.TrimSpace(body.Name))
	if s.Demo != nil && name == s.Demo.Name {
		writeError(w, http.StatusBadRequest, "the demonstration account is set by -demo-account and cannot be changed here")
		return
	}
	if body.SSO {
		if err := s.Sessions.Users().SetSSO(name, body.Role); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("auth: %s set single-sign-on account %s (%s)", actor.User, name, body.Role)
		s.record(actor.User, "set account", fmt.Sprintf("%s: %s, single sign-on", name, body.Role))
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "role": body.Role, "sso": true})
		return
	}
	pw, err := s.Sessions.Users().Set(name, body.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("auth: %s set password account %s (%s)", actor.User, name, body.Role)
	s.record(actor.User, "set account", fmt.Sprintf("%s: %s, new password", name, body.Role))
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "role": body.Role, "password": pw})
}

func (s *Server) removeUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := SessionFrom(r.Context())
	name := r.PathValue("name")
	if name == actor.User {
		writeError(w, http.StatusBadRequest, "you cannot remove your own account while signed in to it")
		return
	}
	if s.Demo != nil && name == s.Demo.Name {
		writeError(w, http.StatusBadRequest, "the demonstration account is set by -demo-account; restart without it to remove it")
		return
	}
	if err := s.Sessions.Users().Remove(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	log.Printf("auth: %s removed account %s", actor.User, name)
	s.record(actor.User, "removed account", name)
	w.WriteHeader(http.StatusNoContent)
}

// activity is the activity log, newest first (admin).
func (s *Server) activity(w http.ResponseWriter, _ *http.Request) {
	log, err := s.Sessions.Users().Store().ActivityLog(500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if log == nil {
		log = []store.Activity{}
	}
	writeJSON(w, http.StatusOK, log)
}

// --- pending single sign-ons -----------------------------------------------------------------------------------------

type pendingSignIn struct {
	nonce, verifier string
	started         time.Time
}

type pending struct {
	mu sync.Mutex
	m  map[string]pendingSignIn
}

func (p *pending) add(key string, s pendingSignIn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.m {
		if time.Since(v.started) > signInWindow {
			delete(p.m, k)
		}
	}
	if len(p.m) > 10000 {
		return false // a flood of abandoned sign-ins: refuse new ones rather than grow without bound
	}
	p.m[key] = s
	return true
}

// take removes and returns a pending sign-in: each is good once.
func (p *pending) take(key string) (pendingSignIn, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.m[key]
	delete(p.m, key)
	if ok && time.Since(s.started) > signInWindow {
		return pendingSignIn{}, false
	}
	return s, ok
}

func (s *Server) ssoSignedIn(w http.ResponseWriter, r *http.Request, name, via string, status int) {
	token, sess, err := s.Sessions.SignInSSO(name, via)
	if err != nil {
		log.Printf("auth: %s sign-in refused for %q: %v", via, name, err)
		http.Redirect(w, r, "/login?sso=unknown", status)
		return
	}
	log.Printf("auth: %s signed in (%s) from %s", sess.User, via, r.RemoteAddr)
	s.record(sess.User, "signed in", via)
	s.setSession(w, token)
	// A Strict session cookie is not sent on this cross-site redirect's landing; the interface's first request, made from
	// its own page, is same-site and carries it.
	http.Redirect(w, r, "/", status)
}

// --- OpenID Connect --------------------------------------------------------------------------------------------------

// oidcStart sends the browser to the provider. The state is also set in a cookie, so the callback is accepted only in
// the browser that started it.
func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		writeError(w, http.StatusNotFound, "OpenID Connect sign-in is not configured")
		return
	}
	state, nonce, verifier := oidc.Random(24), oidc.Random(24), oidc.Random(48)
	if !s.oidcPending.add(state, pendingSignIn{nonce: nonce, verifier: verifier, started: time.Now()}) {
		writeError(w, http.StatusServiceUnavailable, "too many sign-ins are in progress; try again in a few minutes")
		return
	}
	// Lax, not Strict: the provider's redirect back is a cross-site navigation, and a Strict cookie would not come with it.
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: state, Path: "/api/auth/oidc", MaxAge: int(signInWindow.Seconds()),
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.OIDC.Provider.AuthURL(state, nonce, verifier), http.StatusFound)
}

// oidcCallback finishes a sign-in: the code is exchanged, the ID token verified, and the account it names signed in.
// Every refusal is logged and says nothing to the browser that would help someone probing for accounts.
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		writeError(w, http.StatusNotFound, "OpenID Connect sign-in is not configured")
		return
	}
	fail := func(why string) {
		log.Printf("auth: oidc sign-in failed: %s", why)
		http.Redirect(w, r, "/login?sso=failed", http.StatusFound)
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail("the provider answered " + e)
		return
	}
	state := q.Get("state")
	c, err := r.Cookie(oidcCookie)
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1, HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	if err != nil || state == "" || c.Value != state {
		fail("the state does not match the browser's: a sign-in this browser did not start")
		return
	}
	pend, ok := s.oidcPending.take(state)
	if !ok {
		fail("an unknown or expired sign-in")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := s.OIDC.Provider.Exchange(ctx, q.Get("code"), pend.verifier)
	if err != nil {
		fail(err.Error())
		return
	}
	claims, err := s.OIDC.Provider.Verify(ctx, raw, pend.nonce)
	if err != nil {
		fail(err.Error())
		return
	}
	name, err := s.OIDC.Provider.Name(claims)
	if err != nil {
		fail(err.Error())
		return
	}
	s.ssoSignedIn(w, r, name, "oidc", http.StatusFound)
}

// --- SAML 2.0 --------------------------------------------------------------------------------------------------------

func (s *Server) samlStart(w http.ResponseWriter, r *http.Request) {
	if s.SAML == nil {
		writeError(w, http.StatusNotFound, "SAML sign-in is not configured")
		return
	}
	to, id, err := s.SAML.SP.BuildAuthnRequest("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.samlPending.add(id, pendingSignIn{started: time.Now()}) {
		writeError(w, http.StatusServiceUnavailable, "too many sign-ins are in progress; try again in a few minutes")
		return
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// samlACS takes the identity provider's response. Trusted only after its signature (by the configured certificate,
// never one inside the response), audience, destination, conditions, replay and InResponseTo checks all pass.
func (s *Server) samlACS(w http.ResponseWriter, r *http.Request) {
	if s.SAML == nil {
		writeError(w, http.StatusNotFound, "SAML sign-in is not configured")
		return
	}
	fail := func(why string) {
		log.Printf("auth: saml sign-in failed: %s", why)
		http.Redirect(w, r, "/login?sso=failed", http.StatusSeeOther)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	encoded := r.PostFormValue("SAMLResponse")
	if encoded == "" {
		fail("the identity provider sent no response")
		return
	}
	a, err := s.SAML.SP.ParseResponseFor(encoded, func(id string) bool { _, ok := s.samlPending.take(id); return ok })
	if err != nil {
		fail(err.Error())
		return
	}
	if s.SAML.Group != "" && !hasValue(attrValues(a, s.SAML.GroupsAttribute), s.SAML.Group) {
		names := make([]string, 0, len(a.Attributes))
		for n := range a.Attributes {
			names = append(names, n)
		}
		sort.Strings(names)
		// The attributes it did send are logged: reading the wrong one looks exactly like being in no group.
		fail("not a member of " + s.SAML.Group + " (attributes sent: " + strings.Join(names, ", ") + ")")
		return
	}
	name := samlName(a, s.SAML.NameAttribute)
	if name == "" {
		fail("the assertion names nobody")
		return
	}
	s.ssoSignedIn(w, r, name, "saml", http.StatusSeeOther)
}

// samlMetadata is this service provider's metadata, for the identity provider's administrator to import.
func (s *Server) samlMetadata(w http.ResponseWriter, _ *http.Request) {
	if s.SAML == nil {
		writeError(w, http.StatusNotFound, "SAML sign-in is not configured")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true" protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</md:NameIDFormat>
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s" index="0" isDefault="true"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>
`, xmlAttr(s.SAML.EntityID), xmlAttr(s.SAML.ACSURL))
}

func xmlAttr(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(v)
}

// attrValues are an attribute's values, matched by name or by its last part, case aside: providers name one attribute
// by a URI or by a short name.
func attrValues(a *saml.Assertion, want string) []string {
	want = strings.ToLower(want)
	for name, vs := range a.Attributes {
		got := strings.ToLower(name)
		if got == want || strings.HasSuffix(got, "/"+want) {
			return vs
		}
	}
	return nil
}

func hasValue(vs []string, want string) bool {
	for _, v := range vs {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func firstAttr(a *saml.Assertion, names ...string) string {
	for _, n := range names {
		for _, v := range attrValues(a, n) {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	return ""
}

// samlName is the account name an assertion vouches for (oidc.AccountName: lower cased, before any @): an email
// address; then a user principal name, where Entra ID puts it, taken only when it has an @; then the NameID.
func samlName(a *saml.Assertion, attr string) string {
	if attr != "" {
		return oidc.AccountName(firstAttr(a, attr))
	}
	if v := firstAttr(a, "email", "mail", "emailaddress"); v != "" {
		return oidc.AccountName(v)
	}
	if v := firstAttr(a, "upn", "userprincipalname", "name"); strings.Contains(v, "@") {
		return oidc.AccountName(v)
	}
	return oidc.AccountName(a.NameID)
}

// --- helpers ---------------------------------------------------------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
