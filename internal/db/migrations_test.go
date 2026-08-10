package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// newTestStore returns a Store backed by a temp-file database with no migrations
// applied, for exercising migrate() directly.
//
// Deliberately a file rather than :memory:. sql.DB is a *pool*, and every
// connection to :memory: gets its own private database -- so a second pooled
// connection would silently see an empty schema. The shared-cache DSN avoids
// that but ties the database's lifetime to having at least one connection open,
// which a pool that reaps idle connections does not promise. A temp file has
// neither problem and matches production besides.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return &Store{DB: db, log: discardLogger()}
}

// withMigrations swaps the package-level migration list for the duration of one
// test, so migrator *logic* can be exercised without depending on the real
// schema -- which changes every time a migration is added.
//
// Mutating package state means these tests must not call t.Parallel().
func withMigrations(t *testing.T, m []string) {
	t.Helper()

	original := migrations
	migrations = m
	t.Cleanup(func() { migrations = original })
}

// mustMigrate runs migrate or fails the test.
func mustMigrate(t *testing.T, s *Store) {
	t.Helper()

	if err := s.migrate(context.Background()); err != nil {
		t.Fatalf("migrate: unexpected error: %v", err)
	}
}

// currentVersion reads the high-water mark migrate uses to decide what to apply.
func currentVersion(t *testing.T, s *Store) int {
	t.Helper()

	var v int
	if err := s.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	return v
}

// hasTable reports whether a table exists, which is how these tests observe
// whether a migration's effects actually landed.
func hasTable(t *testing.T, s *Store, name string) bool {
	t.Helper()

	var count int
	err := s.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("checking for table %q: %v", name, err)
	}
	return count > 0
}

// TestMigrateAppliesRealMigrations runs the actual migration list. It is the
// only test here that will fail when a newly added migration contains bad SQL,
// which today is discoverable only by starting the server.
func TestMigrateAppliesRealMigrations(t *testing.T) {
	s := newTestStore(t)

	mustMigrate(t, s)

	if got, want := currentVersion(t, s), len(migrations); got != want {
		t.Errorf("version = %d, want %d", got, want)
	}

	// Every table the application expects to exist. Listed explicitly rather
	// than derived from the migrations, so that a migration silently failing to
	// create something is caught rather than reflected back.
	for _, table := range []string{"users", "projects", "issues", "labels", "issue_labels", "api_tokens"} {
		if !hasTable(t, s, table) {
			t.Errorf("table %q missing after migrate", table)
		}
	}
}

// TestMigrateIsIdempotent is the guard that matters most in this file. The
// version number is the *slice index*, so nothing in the type system stops a
// refactor from re-running migrations that already ran -- the failure would be
// silent on a fresh database and destructive on a real one.
func TestMigrateIsIdempotent(t *testing.T) {
	s := newTestStore(t)

	mustMigrate(t, s)
	first := currentVersion(t, s)

	mustMigrate(t, s)

	if got := currentVersion(t, s); got != first {
		t.Errorf("version after second migrate = %d, want %d unchanged", got, first)
	}
}

// TestMigrateAppliesOnlyPending is the upgrade path: a database written by an
// older binary meeting a newer one. Only the new migration may run.
func TestMigrateAppliesOnlyPending(t *testing.T) {
	s := newTestStore(t)

	withMigrations(t, []string{`CREATE TABLE first (id TEXT PRIMARY KEY)`})
	mustMigrate(t, s)

	// Append one, exactly as adding a migration in production does.
	withMigrations(t, []string{
		`CREATE TABLE first (id TEXT PRIMARY KEY)`,
		`CREATE TABLE second (id TEXT PRIMARY KEY)`,
	})
	mustMigrate(t, s)

	if got, want := currentVersion(t, s), 2; got != want {
		t.Errorf("version = %d, want %d", got, want)
	}
	if !hasTable(t, s, "second") {
		t.Error("table \"second\" missing, want the pending migration applied")
	}

	// The first migration lacks IF NOT EXISTS, so re-running it would have
	// errored out above. Reaching here at version 2 proves it was skipped.
}

// TestMigrateRollsBackFailedMigration pins the transaction wrapper. SQLite is
// one of the few engines with transactional DDL -- a CREATE TABLE inside BEGIN
// really does roll back -- and the whole no-half-applied-migration promise rests
// on that. MySQL would not behave this way.
func TestMigrateRollsBackFailedMigration(t *testing.T) {
	s := newTestStore(t)

	withMigrations(t, []string{
		`CREATE TABLE good (id TEXT PRIMARY KEY)`,
		// Two statements: the first succeeds, the second cannot. The rollback
		// has to undo work that already ran inside this transaction.
		`CREATE TABLE partial (id TEXT PRIMARY KEY); INSERT INTO nonexistent VALUES (1)`,
	})

	if err := s.migrate(context.Background()); err == nil {
		t.Fatal("migrate returned nil, want an error from the bad migration")
	}

	if got, want := currentVersion(t, s), 1; got != want {
		t.Errorf("version = %d, want %d -- the failed migration must not be recorded", got, want)
	}
	if hasTable(t, s, "partial") {
		t.Error("table \"partial\" survived a failed migration, want it rolled back")
	}
	if !hasTable(t, s, "good") {
		t.Error("table \"good\" missing, want the earlier migration to have committed")
	}
}

// TestMigrateWhenDatabaseIsAhead covers rolling back a deployment: the data
// directory was written by a binary that knew more migrations than this one.
//
// Current behaviour is to warn and continue, on the reasoning that additive
// migrations are safe to run older code against and refusing would make
// rollbacks impossible. This test pins that choice so it cannot change by
// accident -- if it ever becomes an error, this is the test to update first.
func TestMigrateWhenDatabaseIsAhead(t *testing.T) {
	s := newTestStore(t)

	withMigrations(t, []string{`CREATE TABLE first (id TEXT PRIMARY KEY)`})
	mustMigrate(t, s)

	// Pretend a newer binary applied a migration this one has never heard of.
	if _, err := s.Exec("INSERT INTO schema_migrations (version) VALUES (?)", 2); err != nil {
		t.Fatalf("seeding a future version: %v", err)
	}

	if err := s.migrate(context.Background()); err != nil {
		t.Fatalf("migrate: unexpected error: %v", err)
	}

	if got, want := currentVersion(t, s), 2; got != want {
		t.Errorf("version = %d, want %d left untouched", got, want)
	}
}

// TestAPITokensRejectsDuplicateHash pins the UNIQUE constraint on token_hash.
// Two random tokens will never collide; the constraint is there to make a *bug*
// loud -- if generation ever silently produces a constant or an empty string,
// this is what turns "two users share one credential" into a failed insert.
func TestAPITokensRejectsDuplicateHash(t *testing.T) {
	s := mustOpen(t, t.TempDir())

	if _, err := s.Exec(
		`INSERT INTO users (id, email, name) VALUES (?, ?, ?)`,
		"u1", "user@example.com", "Austin",
	); err != nil {
		t.Fatalf("seeding user: %v", err)
	}

	insert := func(id string) error {
		_, err := s.Exec(
			`INSERT INTO api_tokens (id, user_id, token_hash, name) VALUES (?, ?, ?, ?)`,
			id, "u1", "the-same-hash", "laptop",
		)
		return err
	}

	if err := insert("tok1"); err != nil {
		t.Fatalf("first insert: unexpected error: %v", err)
	}
	if err := insert("tok2"); err == nil {
		t.Error("second insert with a duplicate token_hash succeeded, want a uniqueness violation")
	}
}
