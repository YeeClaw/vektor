package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)


var timeFormat string = "2006-01-02 15:04:05"

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

type APIToken struct {
	TokenID, TokenName, UserID, UserName, Email string
	ExpiresAt                                   *time.Time
}

var ErrTokenNotFound = errors.New("api token not found")

// LookupAPIToken searches for a given (hashed) API token and returns the joined user
// with respective claims.
func (s *Store) LookupAPIToken(ctx context.Context, hashedToken string) (*APIToken, error) {
	row := s.QueryRowContext(ctx,
		`
		SELECT t.id, t.name, t.expires_at, u.id, u.name, u.email
		FROM   api_tokens t
		JOIN   users u ON u.id = t.user_id
		WHERE  t.token_hash = ?
		`,
		hashedToken,
	)

	var owner APIToken
	err := row.Scan(
		&owner.TokenID,
		&owner.TokenName,
		&owner.ExpiresAt,
		&owner.UserID,
		&owner.UserName,
		&owner.Email,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to query for the provided token: %w", err)
	}

	return &owner, nil
}

type CreateAPITokenInput struct {
	UserID, HashedToken, TokenName string
	ExpiresAt                      *time.Time
}

// CreateAPIToken takes a given (hashed) API token and inserts it into the database.
func (s *Store) CreateAPIToken(ctx context.Context, input CreateAPITokenInput) (string, error) {
	id := uuid.New().String()

	var expiresAt sql.NullString
	if input.ExpiresAt != nil {
		expiresAt = sql.NullString{
			String: input.ExpiresAt.UTC().Format(timeFormat),
			Valid:  true,
		}
	}

	_, err := s.ExecContext(ctx,
		`
		INSERT INTO api_tokens (id, user_id, token_hash, name, expires_at)
		VALUES (?, ?, ?, ?, ?)
		`,
		id,
		input.UserID,
		input.HashedToken,
		input.TokenName,
		expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("unable to insert api token record: %w", err)
	}

	return id, nil
}
