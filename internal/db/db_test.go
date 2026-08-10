package db

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// discardLogger is for tests that need a working logger but do not assert on its
// output. Open and migrate both log unconditionally, so a nil logger panics.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// mustOpen opens a Store in a fresh temp directory. The directory is removed by
// the testing package, so each call is fully isolated from every other.
func mustOpen(t *testing.T, dataDir string) *Store {
	t.Helper()

	s, err := Open(context.Background(), dataDir, discardLogger())
	if err != nil {
		t.Fatalf("Open(%q): unexpected error: %v", dataDir, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// pragma reads a single-value PRAGMA back off the connection.
func pragma(t *testing.T, s *Store, name string) string {
	t.Helper()

	var v string
	if err := s.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: unexpected error: %v", name, err)
	}
	return v
}

func TestOpenCreatesDataDirectory(t *testing.T) {
	// Nested path: MkdirAll's job is the whole chain, not just the leaf.
	dataDir := filepath.Join(t.TempDir(), "nested", "data")

	mustOpen(t, dataDir)

	if _, err := os.Stat(filepath.Join(dataDir, "vektor.db")); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}

// TestOpenSetsPragmas is a regression guard, not a feature test. The DSN
// originally used mattn/go-sqlite3 syntax (?_foreign_keys=on&_journal_mode=WAL)
// against the modernc driver, which ignores unknown parameters *silently* --
// foreign keys were off and the journal was in delete mode, with nothing in any
// log to say so. Reading the pragmas back is the only way that regression
// announces itself.
func TestOpenSetsPragmas(t *testing.T) {
	s := mustOpen(t, t.TempDir())

	tests := []struct {
		pragma string
		want   string
	}{
		{"foreign_keys", "1"},
		{"journal_mode", "wal"},
	}

	for _, tt := range tests {
		t.Run(tt.pragma, func(t *testing.T) {
			if got := pragma(t, s, tt.pragma); got != tt.want {
				t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
}

// TestForeignKeysAreEnforced is the behavioural half of TestOpenSetsPragmas.
// Reading the pragma proves the setting took; this proves the setting does
// something. Without it, a driver that reports foreign_keys=1 while ignoring
// REFERENCES would still pass.
func TestForeignKeysAreEnforced(t *testing.T) {
	s := mustOpen(t, t.TempDir())

	_, err := s.Exec(
		`INSERT INTO api_tokens (id, user_id, token_hash, name) VALUES (?, ?, ?, ?)`,
		"tok1", "no-such-user", "hash1", "laptop",
	)
	if err == nil {
		t.Fatal("inserted an api_token for a nonexistent user, want a foreign key violation")
	}
}

// TestOpenRunsMigrations pins the contract Open advertises: what you get back is
// migrated and ready, not merely connected.
func TestOpenRunsMigrations(t *testing.T) {
	s := mustOpen(t, t.TempDir())

	if got, want := currentVersion(t, s), len(migrations); got != want {
		t.Errorf("schema version after Open = %d, want %d", got, want)
	}
}

// TestOpenOnExistingDatabase covers the ordinary case -- a server restarting
// against the data directory it wrote last time. The second Open must be a no-op
// migration-wise, and must not fail.
func TestOpenOnExistingDatabase(t *testing.T) {
	dataDir := t.TempDir()

	first := mustOpen(t, dataDir)
	before := currentVersion(t, first)
	if err := first.Close(); err != nil {
		t.Fatalf("closing first store: %v", err)
	}

	second := mustOpen(t, dataDir)

	if got := currentVersion(t, second); got != before {
		t.Errorf("schema version after reopen = %d, want %d", got, before)
	}
}
