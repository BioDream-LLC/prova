package auth

import (
	"path/filepath"
	"testing"

	"github.com/biodream-llc/prova/internal/store"
)

func init() { Iterations = 1000 } // fast hashing for tests

func open(t *testing.T) *Users {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "prova.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewUsers(st)
}

func TestPasswordSignIn(t *testing.T) {
	u := open(t)
	pw, err := u.Set("alice", Operator)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSessions(u)
	token, sess, err := s.SignIn("alice", pw)
	if err != nil || sess.Role != Operator || sess.Via != "password" {
		t.Fatalf("sign in: %v %+v", err, sess)
	}
	if got, ok := s.Get(token); !ok || got.User != "alice" {
		t.Fatal("session not found")
	}
	if _, _, err := s.SignIn("alice", "wrong"); err != ErrBadCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.SignIn("nobody", pw); err != ErrBadCredentials {
		t.Fatalf("unknown name must look like a wrong password: %v", err)
	}
	s.SignOut(token)
	if _, ok := s.Get(token); ok {
		t.Fatal("signed-out session still valid")
	}
}

func TestLockout(t *testing.T) {
	u := open(t)
	pw, _ := u.Set("bob", Viewer)
	s := NewSessions(u)
	for i := 0; i < maxFailures; i++ {
		s.SignIn("bob", "nope")
	}
	if _, _, err := s.SignIn("bob", pw); err != ErrLocked {
		t.Fatalf("expected lockout, got %v", err)
	}
}

func TestRoleChangeEndsSession(t *testing.T) {
	u := open(t)
	pw, _ := u.Set("carol", Admin)
	s := NewSessions(u)
	token, _, _ := s.SignIn("carol", pw)
	if err := u.SetSSO("carol", Viewer); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(token); ok {
		t.Fatal("a session must end when its account's grant changes")
	}
}

func TestSSOAccounts(t *testing.T) {
	u := open(t)
	if err := u.SetSSO("dana", Viewer); err != nil {
		t.Fatal(err)
	}
	s := NewSessions(u)
	if _, _, err := s.SignInSSO("dana", "oidc"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SignInSSO("eve", "oidc"); err != ErrNotSSO {
		t.Fatalf("an unknown SSO name must be refused, not created: %v", err)
	}
	if _, _, err := s.SignIn("dana", ""); err != ErrBadCredentials {
		t.Fatalf("an SSO-only account has no password: %v", err)
	}
}

func TestRoles(t *testing.T) {
	if !Admin.Can(Operator) || !Operator.Can(Viewer) || Viewer.Can(Operator) || Role("x").Can(Viewer) {
		t.Fatal("role ladder wrong")
	}
	if _, err := open(t).Set("Bad Name", Viewer); err == nil {
		t.Fatal("invalid name accepted")
	}
}

// TestSessionsSurviveARestart: a session is in the database, so a new session table over the same database (a
// restarted server) still knows it — and the database holds only the token's hash.
func TestSessionsSurviveARestart(t *testing.T) {
	u := open(t)
	pw, _ := u.Set("erin", Operator)
	token, _, err := NewSessions(u).SignIn("erin", pw)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := NewSessions(u).Get(token); !ok || got.User != "erin" {
		t.Fatal("a session did not survive a restart")
	}
	if _, ok, _ := u.Store().Session(token); ok {
		t.Fatal("the raw token is stored")
	}
}

func TestDemoPassword(t *testing.T) {
	u := open(t)
	if err := u.SetPassword("demo", Viewer, "try-prova-now"); err != nil {
		t.Fatal(err)
	}
	if !u.HasPassword("demo", "try-prova-now") || u.HasPassword("demo", "nope") {
		t.Fatal("HasPassword wrong")
	}
	if _, _, err := NewSessions(u).SignIn("demo", "try-prova-now"); err != nil {
		t.Fatal(err)
	}
}
