package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLStore struct {
	db      *sql.DB
	dialect string
}

func NewSQLStore(driverName, dsn, dialect string) (*SQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: missing DSN", ErrInvalidStore)
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	store := &SQLStore{db: db, dialect: dialect}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLStore) Close() error { return s.db.Close() }

func (s *SQLStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (s *SQLStore) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			disabled BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
		`CREATE TABLE IF NOT EXISTS prompt_settings (
			id INTEGER PRIMARY KEY,
			architect_system_prompt TEXT NOT NULL DEFAULT '',
			svg_system_prompt TEXT NOT NULL DEFAULT '',
			architect_model_provider TEXT NOT NULL DEFAULT '',
			architect_model_api_key TEXT NOT NULL DEFAULT '',
			architect_model_base_url TEXT NOT NULL DEFAULT '',
			architect_model_model TEXT NOT NULL DEFAULT '',
			svg_model_provider TEXT NOT NULL DEFAULT '',
			svg_model_api_key TEXT NOT NULL DEFAULT '',
			svg_model_base_url TEXT NOT NULL DEFAULT '',
			svg_model_model TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL,
			updated_by TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

func mapSQLErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if isUniqueErr(err) {
		return ErrAlreadyExists
	}
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (User, error) {
	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return User{}, mapSQLErr(err)
	}
	return user, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type sqlSessionRow struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}
