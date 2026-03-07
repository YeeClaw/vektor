package db

import "database/sql"

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
	`, // Later, let's make sure that other snippets follow this format
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`); err != nil {
		return err
	}

	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&current); err != nil {
		return err
	}

	for i := current; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
