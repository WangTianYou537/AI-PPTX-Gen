package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *SQLStore) NeedsSetup(ctx context.Context) (bool, error) {
	count, err := s.CountAdmins(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *SQLStore) CreateUser(ctx context.Context, input CreateUserInput) (User, error) {
	now := time.Now().UTC()
	role := input.Role
	if role == "" {
		role = RoleUser
	}
	user := User{
		ID:           newID(),
		Email:        normalizeEmail(input.Email),
		PasswordHash: input.PasswordHash,
		Role:         role,
		Disabled:     input.Disabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	query := fmt.Sprintf(
		"INSERT INTO users (id, email, password_hash, role, disabled, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s)",
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7),
	)
	_, err := s.db.ExecContext(ctx, query, user.ID, user.Email, user.PasswordHash, user.Role, boolInt(user.Disabled), user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return User{}, mapSQLErr(err)
	}
	return user, nil
}

func (s *SQLStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	query := fmt.Sprintf("SELECT id, email, password_hash, role, disabled, created_at, updated_at FROM users WHERE lower(email) = %s", s.placeholder(1))
	return scanUser(s.db.QueryRowContext(ctx, query, normalizeEmail(email)))
}

func (s *SQLStore) GetUserByID(ctx context.Context, id string) (User, error) {
	query := fmt.Sprintf("SELECT id, email, password_hash, role, disabled, created_at, updated_at FROM users WHERE id = %s", s.placeholder(1))
	return scanUser(s.db.QueryRowContext(ctx, query, id))
}

func (s *SQLStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, email, password_hash, role, disabled, created_at, updated_at FROM users ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *SQLStore) UpdateUser(ctx context.Context, id string, input UpdateUserInput) (User, error) {
	sets := []string{}
	args := []any{}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = %s", column, s.placeholder(len(args))))
	}
	if input.Email != nil {
		add("email", normalizeEmail(*input.Email))
	}
	if input.PasswordHash != nil {
		add("password_hash", *input.PasswordHash)
	}
	if input.Role != nil {
		add("role", *input.Role)
	}
	if input.Disabled != nil {
		add("disabled", boolInt(*input.Disabled))
	}
	add("updated_at", time.Now().UTC())
	args = append(args, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = %s", strings.Join(sets, ", "), s.placeholder(len(args)))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return User{}, mapSQLErr(err)
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUserByID(ctx, id)
}

func (s *SQLStore) DeleteUser(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM sessions WHERE user_id = %s", s.placeholder(1)), id); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM users WHERE id = %s", s.placeholder(1)), id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) CountAdmins(ctx context.Context) (int, error) {
	query := fmt.Sprintf("SELECT count(*) FROM users WHERE role = %s AND disabled = %s", s.placeholder(1), s.placeholder(2))
	var count int
	if err := s.db.QueryRowContext(ctx, query, RoleAdmin, boolInt(false)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLStore) CreateSession(ctx context.Context, session Session) error {
	query := fmt.Sprintf("INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (%s, %s, %s, %s, %s)", s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5))
	_, err := s.db.ExecContext(ctx, query, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	return mapSQLErr(err)
}

func (s *SQLStore) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	query := fmt.Sprintf("SELECT id, user_id, token_hash, expires_at, created_at FROM sessions WHERE token_hash = %s", s.placeholder(1))
	var session Session
	if err := s.db.QueryRowContext(ctx, query, tokenHash).Scan(&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt); err != nil {
		return Session{}, mapSQLErr(err)
	}
	if session.ExpiresAt.Before(time.Now().UTC()) {
		_ = s.DeleteSession(ctx, tokenHash)
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *SQLStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM sessions WHERE token_hash = %s", s.placeholder(1)), tokenHash)
	return err
}

func (s *SQLStore) GetPromptSettings(ctx context.Context) (PromptSettings, error) {
	var settings PromptSettings
	err := s.db.QueryRowContext(ctx, `SELECT architect_system_prompt, svg_system_prompt,
		architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model,
		svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model,
		updated_at, updated_by FROM prompt_settings WHERE id = 1`).Scan(
		&settings.Architect.SystemPrompt,
		&settings.SVG.SystemPrompt,
		&settings.Architect.ModelConfig.Provider,
		&settings.Architect.ModelConfig.APIKey,
		&settings.Architect.ModelConfig.BaseURL,
		&settings.Architect.ModelConfig.Model,
		&settings.SVG.ModelConfig.Provider,
		&settings.SVG.ModelConfig.APIKey,
		&settings.SVG.ModelConfig.BaseURL,
		&settings.SVG.ModelConfig.Model,
		&settings.UpdatedAt,
		&settings.UpdatedBy,
	)
	if err == sql.ErrNoRows {
		return PromptSettings{}, nil
	}
	if err != nil {
		return PromptSettings{}, err
	}
	return settings, nil
}

func (s *SQLStore) SavePromptSettings(ctx context.Context, settings PromptSettings) error {
	settings.UpdatedAt = time.Now().UTC()
	if s.dialect == "postgres" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO prompt_settings (
			id, architect_system_prompt, svg_system_prompt,
			architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model,
			svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model,
			updated_at, updated_by)
			VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET
			architect_system_prompt = EXCLUDED.architect_system_prompt,
			svg_system_prompt = EXCLUDED.svg_system_prompt,
			architect_model_provider = EXCLUDED.architect_model_provider,
			architect_model_api_key = EXCLUDED.architect_model_api_key,
			architect_model_base_url = EXCLUDED.architect_model_base_url,
			architect_model_model = EXCLUDED.architect_model_model,
			svg_model_provider = EXCLUDED.svg_model_provider,
			svg_model_api_key = EXCLUDED.svg_model_api_key,
			svg_model_base_url = EXCLUDED.svg_model_base_url,
			svg_model_model = EXCLUDED.svg_model_model,
			updated_at = EXCLUDED.updated_at,
			updated_by = EXCLUDED.updated_by`,
			settings.Architect.SystemPrompt, settings.SVG.SystemPrompt,
			settings.Architect.ModelConfig.Provider, settings.Architect.ModelConfig.APIKey, settings.Architect.ModelConfig.BaseURL, settings.Architect.ModelConfig.Model,
			settings.SVG.ModelConfig.Provider, settings.SVG.ModelConfig.APIKey, settings.SVG.ModelConfig.BaseURL, settings.SVG.ModelConfig.Model,
			settings.UpdatedAt, settings.UpdatedBy)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO prompt_settings (
		id, architect_system_prompt, svg_system_prompt,
		architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model,
		svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model,
		updated_at, updated_by)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		architect_system_prompt = excluded.architect_system_prompt,
		svg_system_prompt = excluded.svg_system_prompt,
		architect_model_provider = excluded.architect_model_provider,
		architect_model_api_key = excluded.architect_model_api_key,
		architect_model_base_url = excluded.architect_model_base_url,
		architect_model_model = excluded.architect_model_model,
		svg_model_provider = excluded.svg_model_provider,
		svg_model_api_key = excluded.svg_model_api_key,
		svg_model_base_url = excluded.svg_model_base_url,
		svg_model_model = excluded.svg_model_model,
		updated_at = excluded.updated_at,
		updated_by = excluded.updated_by`,
		settings.Architect.SystemPrompt, settings.SVG.SystemPrompt,
		settings.Architect.ModelConfig.Provider, settings.Architect.ModelConfig.APIKey, settings.Architect.ModelConfig.BaseURL, settings.Architect.ModelConfig.Model,
		settings.SVG.ModelConfig.Provider, settings.SVG.ModelConfig.APIKey, settings.SVG.ModelConfig.BaseURL, settings.SVG.ModelConfig.Model,
		settings.UpdatedAt, settings.UpdatedBy)
	return err
}
