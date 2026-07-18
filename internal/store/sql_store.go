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
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		id INTEGER PRIMARY KEY,
		version INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	version, err := s.schemaVersion()
	if err != nil {
		return err
	}
	for _, step := range sqlMigrations {
		if step.Version <= version {
			continue
		}
		for _, stmt := range step.Statements {
			if _, err := s.db.Exec(stmt); err != nil {
				// Keep legacy ALTER compatibility when upgrading partial old DBs.
				if step.AllowDuplicateColumn && isDuplicateColumnErr(err) {
					continue
				}
				return fmt.Errorf("migration v%d failed: %w", step.Version, err)
			}
		}
		if err := s.setSchemaVersion(step.Version); err != nil {
			return err
		}
		version = step.Version
	}
	return s.ensureDefaults()
}

func (s *SQLStore) schemaVersion() (int, error) {
	var version int
	err := s.db.QueryRow(`SELECT version FROM schema_version WHERE id = 1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func (s *SQLStore) setSchemaVersion(version int) error {
	if s.dialect == "postgres" {
		_, err := s.db.Exec(`INSERT INTO schema_version (id, version) VALUES (1, $1)
			ON CONFLICT (id) DO UPDATE SET version = EXCLUDED.version`, version)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO schema_version (id, version) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET version = excluded.version`, version)
	return err
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
