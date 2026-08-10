package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store is a lightweight and purpose built SQL wrapper designed around the specs
// of this application.
type Store struct {
	*sql.DB
	log *slog.Logger
}

func Open(ctx context.Context, dataDir string, logger *slog.Logger) (*Store, error) {
	log := logger.With("component", "db")

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	log.DebugContext(ctx, "data directory is ready")

	dbPath := filepath.Join(dataDir, "vektor.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	log.DebugContext(ctx, "successfully opened the database")

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	log.DebugContext(ctx, "successfully reached database")

	s := &Store{DB: db, log: log}

	if err := s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	s.log.Debug("closing the database")
	err := s.DB.Close()
	if err != nil {
		// Logging because the error might not surface anywhere depending on how this is called
		s.log.Error("unable to close the database", "error", err)
		return fmt.Errorf("unable to close the database: %w", err)
	}

	return nil
}
