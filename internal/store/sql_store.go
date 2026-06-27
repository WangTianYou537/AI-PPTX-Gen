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
)

type SQLStore struct {
	db      *sql.DB
	dialect string
}

func NewSQLStore(driverName, dsn, dialect string) (*SQLStore, error) {
	return NewSQLStoreContext(context.Background(), driverName, dsn, dialect)
}

func NewSQLStoreContext(ctx context.Context, driverName, dsn, dialect string) (*SQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: missing DSN", ErrInvalidStore)
	}
	if dialect == "sqlite" {
		if dir := filepath.Dir(dsn); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
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
			username TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			disabled BOOLEAN NOT NULL DEFAULT FALSE,
			group_id TEXT NOT NULL DEFAULT 'default',
			daily_ppt_limit INTEGER NULL,
			daily_slide_limit INTEGER NULL,
			slide_concurrency_limit INTEGER NULL,
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
		`CREATE TABLE IF NOT EXISTS system_settings (
			id INTEGER PRIMARY KEY,
			registration_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			default_user_group_id TEXT NOT NULL DEFAULT 'default',
			default_slide_concurrency_limit INTEGER NOT NULL DEFAULT 2,
			updated_at TIMESTAMP NOT NULL,
			updated_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS user_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			daily_ppt_limit INTEGER NOT NULL,
			daily_slide_limit INTEGER NOT NULL,
			slide_concurrency_limit INTEGER NOT NULL DEFAULT 2,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS daily_usage (
			user_id TEXT NOT NULL,
			usage_date TEXT NOT NULL,
			ppt_used INTEGER NOT NULL DEFAULT 0,
			slides_used INTEGER NOT NULL DEFAULT 0,
			ppt_reserved INTEGER NOT NULL DEFAULT 0,
			slides_reserved INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (user_id, usage_date)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	columns := []string{
		"ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE users ADD COLUMN group_id TEXT NOT NULL DEFAULT 'default'",
		"ALTER TABLE users ADD COLUMN daily_ppt_limit INTEGER NULL",
		"ALTER TABLE users ADD COLUMN daily_slide_limit INTEGER NULL",
		"ALTER TABLE users ADD COLUMN slide_concurrency_limit INTEGER NULL",
		"ALTER TABLE user_groups ADD COLUMN slide_concurrency_limit INTEGER NOT NULL DEFAULT 2",
		"ALTER TABLE system_settings ADD COLUMN default_slide_concurrency_limit INTEGER NOT NULL DEFAULT 2",
	}
	for _, stmt := range columns {
		if _, err := s.db.Exec(stmt); err != nil && !isDuplicateColumnErr(err) {
			return err
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_users_group_id ON users(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_usage_date ON daily_usage(usage_date)`,
	}
	for _, stmt := range indexes {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return s.ensureDefaults()
}

func (s *SQLStore) ensureDefaults() error {
	now := time.Now().UTC()
	group := DefaultUserGroup(now)
	if s.dialect == "postgres" {
		_, err := s.db.Exec(`INSERT INTO user_groups (id, name, description, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, is_default, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (id) DO NOTHING`, group.ID, group.Name, group.Description, group.DailyPPTLimit, group.DailySlideLimit, group.SlideConcurrencyLimit, boolInt(group.IsDefault), group.CreatedAt, group.UpdatedAt)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`INSERT INTO system_settings (id, registration_enabled, default_user_group_id, default_slide_concurrency_limit, updated_at, updated_by)
			VALUES (1, TRUE, $1, $2, $3, '') ON CONFLICT (id) DO NOTHING`, group.ID, group.SlideConcurrencyLimit, now)
		if err != nil {
			return err
		}
	} else {
		_, err := s.db.Exec(`INSERT INTO user_groups (id, name, description, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, group.ID, group.Name, group.Description, group.DailyPPTLimit, group.DailySlideLimit, group.SlideConcurrencyLimit, boolInt(group.IsDefault), group.CreatedAt, group.UpdatedAt)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`INSERT INTO system_settings (id, registration_enabled, default_user_group_id, default_slide_concurrency_limit, updated_at, updated_by)
			VALUES (1, ?, ?, ?, ?, '') ON CONFLICT(id) DO NOTHING`, boolInt(true), group.ID, group.SlideConcurrencyLimit, now)
		if err != nil {
			return err
		}
	}
	if _, err := s.db.Exec("UPDATE users SET group_id = 'default' WHERE group_id = ''"); err != nil {
		return err
	}
	rows, err := s.db.Query("SELECT id, email FROM users WHERE username = ''")
	if err != nil {
		return err
	}
	defer rows.Close()
	updates := map[string]string{}
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return err
		}
		updates[id] = DefaultUsername(email)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for id, username := range updates {
		if _, err := s.db.Exec(fmt.Sprintf("UPDATE users SET username = %s WHERE id = %s", s.placeholder(1), s.placeholder(2)), username, id); err != nil {
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

func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
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

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (User, error) {
	var user User
	var pptLimit, slideLimit, concurrencyLimit sql.NullInt64
	if err := row.Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role, &user.Disabled, &user.GroupID, &pptLimit, &slideLimit, &concurrencyLimit, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return User{}, mapSQLErr(err)
	}
	if pptLimit.Valid {
		value := int(pptLimit.Int64)
		user.DailyPPTLimit = &value
	}
	if slideLimit.Valid {
		value := int(slideLimit.Int64)
		user.DailySlideLimit = &value
	}
	if concurrencyLimit.Valid {
		value := int(concurrencyLimit.Int64)
		user.SlideConcurrencyLimit = &value
	}
	if user.Username == "" {
		user.Username = DefaultUsername(user.Email)
	}
	if user.GroupID == "" {
		user.GroupID = DefaultUserGroupID
	}
	return user, nil
}

func scanUserGroup(row scanner) (UserGroup, error) {
	var group UserGroup
	if err := row.Scan(&group.ID, &group.Name, &group.Description, &group.DailyPPTLimit, &group.DailySlideLimit, &group.SlideConcurrencyLimit, &group.IsDefault, &group.CreatedAt, &group.UpdatedAt); err != nil {
		return UserGroup{}, mapSQLErr(err)
	}
	return group, nil
}

func scanDailyUsage(row scanner) (DailyUsage, error) {
	var usage DailyUsage
	if err := row.Scan(&usage.UserID, &usage.Date, &usage.PPTUsed, &usage.SlidesUsed, &usage.PPTReserved, &usage.SlidesReserved, &usage.UpdatedAt); err != nil {
		return DailyUsage{}, mapSQLErr(err)
	}
	return usage, nil
}

const userColumns = "id, email, username, password_hash, role, disabled, group_id, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, created_at, updated_at"

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type sqlSessionRow struct {
	ID, UserID, TokenHash string
	ExpiresAt, CreatedAt  time.Time
}
