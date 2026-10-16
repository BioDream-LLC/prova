// Package store is prova's database: one SQLite file (prova.db in the data directory) holding the accounts, the
// signed-in sessions, every run with its proof, every suite and load run, and the activity log. It is embedded — the
// SQLite engine is compiled into the binary (modernc.org/sqlite, pure Go), so there is no database server to install,
// and a backup is a copy of one file.
//
// Keeping runs here, rather than in memory, is what makes reporting possible: pass rates by day, target and protocol,
// a suite's history, and a CSV of every verdict with its proof, across restarts.
//
// Session tokens are never stored: a session row is keyed by the SHA-256 of its token, so a copy of the database
// cannot be used to sign in as anyone.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/sqlitedb"
)

// Store is the open database.
type Store struct {
	db *sqlitedb.DB
}

// migrations are applied in order; the database's user_version records how many have run. Append, never edit.
var migrations = []string{
	`CREATE TABLE users (
		name       TEXT PRIMARY KEY,
		role       TEXT NOT NULL,
		sso        INTEGER NOT NULL DEFAULT 0,
		salt       TEXT NOT NULL DEFAULT '',
		hash       TEXT NOT NULL DEFAULT '',
		iterations INTEGER NOT NULL DEFAULT 0,
		created    INTEGER NOT NULL
	);
	CREATE TABLE sessions (
		id      TEXT PRIMARY KEY,
		user    TEXT NOT NULL REFERENCES users(name) ON DELETE CASCADE,
		role    TEXT NOT NULL,
		via     TEXT NOT NULL,
		created INTEGER NOT NULL,
		seen    INTEGER NOT NULL
	);
	CREATE INDEX sessions_user ON sessions(user);
	CREATE TABLE runs (
		id          TEXT PRIMARY KEY,
		seq         INTEGER NOT NULL UNIQUE,
		at          INTEGER NOT NULL,
		scenario_id TEXT NOT NULL,
		scenario    TEXT NOT NULL,
		protocol    TEXT NOT NULL,
		target      TEXT NOT NULL,
		outcome     TEXT NOT NULL,
		latency_ms  REAL NOT NULL,
		actor       TEXT NOT NULL DEFAULT '',
		suite_run   TEXT NOT NULL DEFAULT '',
		body        TEXT NOT NULL
	);
	CREATE INDEX runs_at ON runs(at);
	CREATE TABLE suite_runs (
		id          TEXT PRIMARY KEY,
		suite_id    TEXT NOT NULL,
		name        TEXT NOT NULL,
		target      TEXT NOT NULL,
		at          INTEGER NOT NULL,
		outcome     TEXT NOT NULL,
		pass        INTEGER NOT NULL,
		fail        INTEGER NOT NULL,
		indet       INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL,
		actor       TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX suite_runs_at ON suite_runs(at);
	CREATE TABLE load_runs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		at          INTEGER NOT NULL,
		scenario    TEXT NOT NULL,
		target      TEXT NOT NULL,
		n           INTEGER NOT NULL,
		concurrency INTEGER NOT NULL,
		throughput  REAL NOT NULL,
		p50 REAL NOT NULL, p95 REAL NOT NULL, p99 REAL NOT NULL, max REAL NOT NULL,
		errors      INTEGER NOT NULL,
		actor       TEXT NOT NULL DEFAULT '',
		body        TEXT NOT NULL
	);
	CREATE TABLE activity (
		id     INTEGER PRIMARY KEY AUTOINCREMENT,
		at     INTEGER NOT NULL,
		actor  TEXT NOT NULL,
		action TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX activity_at ON activity(at);`,
}

// Open opens (creating if need be) the database at path and brings its schema up to date.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sqlitedb.Open(path, sqlitedb.Options{})
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if path != ":memory:" {
		_ = os.Chmod(path, 0o600) // accounts and sessions live here
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Path is the database file.
func (s *Store) Path() string { return s.db.Path }

func (s *Store) migrate() error {
	var v int
	if err := s.db.Write.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("store: reading the schema version: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("store: %s was written by a newer prova (schema %d, this build knows %d)", s.db.Path, v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Write.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func ms(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

// --- accounts --------------------------------------------------------------------------------------------------------

// User is one account row.
type User struct {
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	SSO        bool      `json:"sso,omitempty"`
	Salt       string    `json:"-"`
	Hash       string    `json:"-"`
	Iterations int       `json:"-"`
	Created    time.Time `json:"created"`
}

// ErrNoUser is an account that does not exist.
var ErrNoUser = errors.New("no such account")

// Users lists every account, by name.
func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Read.Query(`SELECT name, role, sso, salt, hash, iterations, created FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created int64
		if err := rows.Scan(&u.Name, &u.Role, &u.SSO, &u.Salt, &u.Hash, &u.Iterations, &created); err != nil {
			return nil, err
		}
		u.Created = fromMS(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// User returns one account.
func (s *Store) User(name string) (User, bool, error) {
	var u User
	var created int64
	err := s.db.Read.QueryRow(`SELECT name, role, sso, salt, hash, iterations, created FROM users WHERE name = ?`, name).
		Scan(&u.Name, &u.Role, &u.SSO, &u.Salt, &u.Hash, &u.Iterations, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	u.Created = fromMS(created)
	return u, true, nil
}

// PutUser creates or replaces an account, keeping its creation time. Its sessions end unless keepSessions: a changed
// role or password must not leave an old grant signed in.
func (s *Store) PutUser(u User, keepSessions bool) error {
	tx, err := s.db.Write.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO users (name, role, sso, salt, hash, iterations, created) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET role = excluded.role, sso = excluded.sso, salt = excluded.salt,
			hash = excluded.hash, iterations = excluded.iterations`,
		u.Name, u.Role, u.SSO, u.Salt, u.Hash, u.Iterations, ms(u.Created)); err != nil {
		return err
	}
	if !keepSessions {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user = ?`, u.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteUser removes an account and, with it, its sessions.
func (s *Store) DeleteUser(name string) error {
	res, err := s.db.Write.Exec(`DELETE FROM users WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %q", ErrNoUser, name)
	}
	return nil
}

// CountUsers is the number of accounts.
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.Read.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// --- sessions --------------------------------------------------------------------------------------------------------

// Session is one signed-in session row. ID is the SHA-256 of the token, never the token.
type Session struct {
	ID      string
	User    string
	Role    string
	Via     string
	Created time.Time
	Seen    time.Time
}

// PutSession records a new session.
func (s *Store) PutSession(x Session) error {
	_, err := s.db.Write.Exec(`INSERT INTO sessions (id, user, role, via, created, seen) VALUES (?, ?, ?, ?, ?, ?)`,
		x.ID, x.User, x.Role, x.Via, ms(x.Created), ms(x.Seen))
	return err
}

// Session returns a session by id.
func (s *Store) Session(id string) (Session, bool, error) {
	var x Session
	var created, seen int64
	err := s.db.Read.QueryRow(`SELECT id, user, role, via, created, seen FROM sessions WHERE id = ?`, id).
		Scan(&x.ID, &x.User, &x.Role, &x.Via, &created, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	x.Created, x.Seen = fromMS(created), fromMS(seen)
	return x, true, nil
}

// TouchSession records activity on a session.
func (s *Store) TouchSession(id string, seen time.Time) error {
	_, err := s.db.Write.Exec(`UPDATE sessions SET seen = ? WHERE id = ?`, ms(seen), id)
	return err
}

// DeleteSession ends a session.
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Write.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// PruneSessions removes sessions idle since before idleBefore or created before createdBefore.
func (s *Store) PruneSessions(idleBefore, createdBefore time.Time) error {
	_, err := s.db.Write.Exec(`DELETE FROM sessions WHERE seen < ? OR created < ?`, ms(idleBefore), ms(createdBefore))
	return err
}

// --- runs ------------------------------------------------------------------------------------------------------------

// Run is one stored scenario run. Body is the full run as JSON (messages, proof, certificate), so a stored run renders
// on the Proof screen exactly as it did when it ran.
type Run struct {
	ID         string
	Seq        int
	At         time.Time
	ScenarioID string
	Scenario   string
	Protocol   string
	Target     string
	Outcome    string
	LatencyMs  float64
	Actor      string
	SuiteRun   string
	Body       []byte
}

// PutRun stores a run.
func (s *Store) PutRun(r Run) error {
	_, err := s.db.Write.Exec(`INSERT INTO runs (id, seq, at, scenario_id, scenario, protocol, target, outcome, latency_ms,
		actor, suite_run, body) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Seq, ms(r.At), r.ScenarioID, r.Scenario, r.Protocol, r.Target, r.Outcome, r.LatencyMs, r.Actor, r.SuiteRun,
		string(r.Body))
	return err
}

// SetRunSuite links stored runs to the suite run they were part of.
func (s *Store) SetRunSuite(suiteRun string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{suiteRun}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Write.Exec(`UPDATE runs SET suite_run = ? WHERE id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args...)
	return err
}

// RecentRuns returns up to n runs, newest first.
func (s *Store) RecentRuns(n int) ([]Run, error) {
	return s.queryRuns(`SELECT id, seq, at, scenario_id, scenario, protocol, target, outcome, latency_ms, actor, suite_run, body
		FROM runs ORDER BY seq DESC LIMIT ?`, n)
}

// RunsSince returns every run at or after since, oldest first, without bodies unless withBody.
func (s *Store) RunsSince(since time.Time, withBody bool) ([]Run, error) {
	body := "''"
	if withBody {
		body = "body"
	}
	return s.queryRuns(`SELECT id, seq, at, scenario_id, scenario, protocol, target, outcome, latency_ms, actor, suite_run, `+
		body+` FROM runs WHERE at >= ? ORDER BY seq`, ms(since))
}

// RunByID returns one stored run.
func (s *Store) RunByID(id string) (Run, bool, error) {
	rs, err := s.queryRuns(`SELECT id, seq, at, scenario_id, scenario, protocol, target, outcome, latency_ms, actor, suite_run, body
		FROM runs WHERE id = ?`, id)
	if err != nil || len(rs) == 0 {
		return Run{}, false, err
	}
	return rs[0], true, nil
}

func (s *Store) queryRuns(q string, args ...any) ([]Run, error) {
	rows, err := s.db.Read.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var at int64
		var body string
		if err := rows.Scan(&r.ID, &r.Seq, &at, &r.ScenarioID, &r.Scenario, &r.Protocol, &r.Target, &r.Outcome, &r.LatencyMs,
			&r.Actor, &r.SuiteRun, &body); err != nil {
			return nil, err
		}
		r.At, r.Body = fromMS(at), []byte(body)
		out = append(out, r)
	}
	return out, rows.Err()
}

// MaxSeq is the highest run and suite-run sequence number stored, so numbering continues across restarts.
func (s *Store) MaxSeq() (int, error) {
	var n sql.NullInt64
	err := s.db.Read.QueryRow(`SELECT max(seq) FROM runs`).Scan(&n)
	return int(n.Int64), err
}

// CountRuns is the number of stored runs.
func (s *Store) CountRuns() (int, error) {
	var n int
	err := s.db.Read.QueryRow(`SELECT count(*) FROM runs`).Scan(&n)
	return n, err
}

// SuiteRun is one stored suite execution.
type SuiteRun struct {
	ID         string    `json:"id"`
	SuiteID    string    `json:"suiteId"`
	Name       string    `json:"name"`
	Target     string    `json:"target"`
	At         time.Time `json:"at"`
	Outcome    string    `json:"outcome"`
	Pass       int       `json:"pass"`
	Fail       int       `json:"fail"`
	Indet      int       `json:"indeterminate"`
	DurationMs int64     `json:"durationMs"`
	Actor      string    `json:"actor,omitempty"`
}

// PutSuiteRun stores a suite execution.
func (s *Store) PutSuiteRun(x SuiteRun) error {
	_, err := s.db.Write.Exec(`INSERT INTO suite_runs (id, suite_id, name, target, at, outcome, pass, fail, indet, duration_ms, actor)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.ID, x.SuiteID, x.Name, x.Target, ms(x.At), x.Outcome, x.Pass, x.Fail, x.Indet, x.DurationMs, x.Actor)
	return err
}

// SuiteRuns returns suite executions at or after since, newest first, at most n.
func (s *Store) SuiteRuns(since time.Time, n int) ([]SuiteRun, error) {
	rows, err := s.db.Read.Query(`SELECT id, suite_id, name, target, at, outcome, pass, fail, indet, duration_ms, actor
		FROM suite_runs WHERE at >= ? ORDER BY at DESC LIMIT ?`, ms(since), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuiteRun
	for rows.Next() {
		var x SuiteRun
		var at int64
		if err := rows.Scan(&x.ID, &x.SuiteID, &x.Name, &x.Target, &at, &x.Outcome, &x.Pass, &x.Fail, &x.Indet,
			&x.DurationMs, &x.Actor); err != nil {
			return nil, err
		}
		x.At = fromMS(at)
		out = append(out, x)
	}
	return out, rows.Err()
}

// LoadRun is one stored load test.
type LoadRun struct {
	At          time.Time `json:"at"`
	Scenario    string    `json:"scenario"`
	Target      string    `json:"target"`
	N           int       `json:"n"`
	Concurrency int       `json:"concurrency"`
	Throughput  float64   `json:"throughput"`
	P50         float64   `json:"p50"`
	P95         float64   `json:"p95"`
	P99         float64   `json:"p99"`
	Max         float64   `json:"max"`
	Errors      int       `json:"errors"`
	Actor       string    `json:"actor,omitempty"`
	Body        []byte    `json:"-"`
}

// PutLoadRun stores a load test.
func (s *Store) PutLoadRun(x LoadRun) error {
	_, err := s.db.Write.Exec(`INSERT INTO load_runs (at, scenario, target, n, concurrency, throughput, p50, p95, p99, max,
		errors, actor, body) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ms(x.At), x.Scenario, x.Target, x.N, x.Concurrency, x.Throughput, x.P50, x.P95, x.P99, x.Max, x.Errors, x.Actor,
		string(x.Body))
	return err
}

// LoadRuns returns load tests at or after since, newest first, at most n.
func (s *Store) LoadRuns(since time.Time, n int) ([]LoadRun, error) {
	rows, err := s.db.Read.Query(`SELECT at, scenario, target, n, concurrency, throughput, p50, p95, p99, max, errors, actor
		FROM load_runs WHERE at >= ? ORDER BY at DESC LIMIT ?`, ms(since), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoadRun
	for rows.Next() {
		var x LoadRun
		var at int64
		if err := rows.Scan(&at, &x.Scenario, &x.Target, &x.N, &x.Concurrency, &x.Throughput, &x.P50, &x.P95, &x.P99,
			&x.Max, &x.Errors, &x.Actor); err != nil {
			return nil, err
		}
		x.At = fromMS(at)
		out = append(out, x)
	}
	return out, rows.Err()
}

// Prune deletes runs, suite runs, load runs and activity older than before. It returns the number of runs removed.
func (s *Store) Prune(ctx context.Context, before time.Time) (int64, error) {
	t := ms(before)
	res, err := s.db.Write.ExecContext(ctx, `DELETE FROM runs WHERE at < ?`, t)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, q := range []string{`DELETE FROM suite_runs WHERE at < ?`, `DELETE FROM load_runs WHERE at < ?`,
		`DELETE FROM activity WHERE at < ?`} {
		if _, err := s.db.Write.ExecContext(ctx, q, t); err != nil {
			return n, err
		}
	}
	return n, nil
}

// --- activity --------------------------------------------------------------------------------------------------------

// Activity is one entry in the activity log: who did what, and when.
type Activity struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail,omitempty"`
}

// Log records an activity. A failure to record is returned, not hidden; callers log it.
func (s *Store) Log(actor, action, detail string) error {
	_, err := s.db.Write.Exec(`INSERT INTO activity (at, actor, action, detail) VALUES (?, ?, ?, ?)`,
		ms(time.Now()), actor, action, detail)
	return err
}

// ActivityLog returns up to n entries, newest first.
func (s *Store) ActivityLog(n int) ([]Activity, error) {
	rows, err := s.db.Read.Query(`SELECT at, actor, action, detail FROM activity ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		var at int64
		if err := rows.Scan(&at, &a.Actor, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		a.At = fromMS(at)
		out = append(out, a)
	}
	return out, rows.Err()
}
