package db

import (
	"context"
	"time"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		avatar_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		key TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		description TEXT,
		created_by TEXT NOT NULL REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS issues (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL REFERENCES projects(id),
		number INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		status TEXT NOT NULL DEFAULT 'backlog',
		priority TEXT NOT NULL DEFAULT 'none',
		assignee_id TEXT REFERENCES users(id),
		created_by TEXT NOT NULL REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(project_id, number)
	)`,
	`CREATE TABLE IF NOT EXISTS labels (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL REFERENCES projects(id),
		name TEXT NOT NULL,
		color TEXT NOT NULL,
		UNIQUE(project_id, name)
	)`,
	`CREATE TABLE IF NOT EXISTS issue_labels (
		issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
		label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
		PRIMARY KEY (issue_id, label_id)
	)`,
	`
	ALTER TABLE users ADD COLUMN password_hash TEXT
	`,
	`CREATE TABLE IF NOT EXISTS api_tokens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id),
		token_hash TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME
	)`, // nullable expiration means no expiration on null values
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`)
	if err != nil {
		return err
	}
	s.log.DebugContext(ctx, "executed schema_migrations table creation")

	var current int
	row := s.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&current); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "found current migration version", "current", current)

	if current == len(migrations) {
		s.log.DebugContext(ctx, "up-to-date on migrations")
	} else if current > len(migrations) {
		s.log.WarnContext(ctx, "local database is ahead of migrations", "ahead", (current - len(migrations)))
	} else {
		s.log.InfoContext(ctx, "found pending migrations", "pending", (len(migrations) - current))
	}

	for i := current; i < len(migrations); i++ {
		start := time.Now()

		tx, err := s.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", i+1); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
		s.log.InfoContext(ctx, "applied migration transaction", "version", (i + 1), "duration", time.Since(start))
	}

	return nil
}
