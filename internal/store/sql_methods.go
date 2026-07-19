package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	createdAt := now
	updatedAt := now
	if input.CreatedAt != nil {
		createdAt = *input.CreatedAt
	}
	if input.UpdatedAt != nil {
		updatedAt = *input.UpdatedAt
	}
	role := input.Role
	if role == "" {
		role = RoleUser
	}
	id := input.ID
	if id == "" {
		id = newID()
	}
	email := normalizeEmail(input.Email)
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = DefaultUsername(email)
	}
	groupID := input.GroupID
	if groupID == "" {
		settings, err := s.GetSystemSettings(ctx)
		if err != nil {
			return User{}, err
		}
		groupID = settings.DefaultUserGroupID
	}
	if _, err := s.GetUserGroup(ctx, groupID); err != nil {
		return User{}, err
	}
	user := User{ID: id, Email: email, Username: username, PasswordHash: input.PasswordHash, Role: role, Disabled: input.Disabled, GroupID: groupID, DailyPPTLimit: cloneIntPtr(input.DailyPPTLimit), DailySlideLimit: cloneIntPtr(input.DailySlideLimit), SlideConcurrencyLimit: cloneIntPtr(input.SlideConcurrencyLimit), CreatedAt: createdAt, UpdatedAt: updatedAt}
	query := fmt.Sprintf(
		"INSERT INTO users (id, email, username, password_hash, role, disabled, group_id, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)",
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9), s.placeholder(10), s.placeholder(11), s.placeholder(12),
	)
	_, err := s.db.ExecContext(ctx, query, user.ID, user.Email, user.Username, user.PasswordHash, user.Role, boolInt(user.Disabled), user.GroupID, nullableInt(user.DailyPPTLimit), nullableInt(user.DailySlideLimit), nullableInt(user.SlideConcurrencyLimit), user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return User{}, mapSQLErr(err)
	}
	return user, nil
}

func (s *SQLStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	query := fmt.Sprintf("SELECT %s FROM users WHERE lower(email) = %s", userColumns, s.placeholder(1))
	return scanUser(s.db.QueryRowContext(ctx, query, normalizeEmail(email)))
}

func (s *SQLStore) GetUserByID(ctx context.Context, id string) (User, error) {
	query := fmt.Sprintf("SELECT %s FROM users WHERE id = %s", userColumns, s.placeholder(1))
	return scanUser(s.db.QueryRowContext(ctx, query, id))
}

func (s *SQLStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY created_at ASC")
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
	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if username == "" {
			current, err := s.GetUserByID(ctx, id)
			if err != nil {
				return User{}, err
			}
			username = DefaultUsername(current.Email)
		}
		add("username", username)
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
	if input.GroupID != nil {
		if _, err := s.GetUserGroup(ctx, *input.GroupID); err != nil {
			return User{}, err
		}
		add("group_id", *input.GroupID)
	}
	if input.ClearDailyPPTLimit {
		sets = append(sets, "daily_ppt_limit = NULL")
	} else if input.DailyPPTLimit != nil {
		add("daily_ppt_limit", *input.DailyPPTLimit)
	}
	if input.ClearDailySlideLimit {
		sets = append(sets, "daily_slide_limit = NULL")
	} else if input.DailySlideLimit != nil {
		add("daily_slide_limit", *input.DailySlideLimit)
	}
	if input.ClearSlideConcurrencyLimit {
		sets = append(sets, "slide_concurrency_limit = NULL")
	} else if input.SlideConcurrencyLimit != nil {
		add("slide_concurrency_limit", *input.SlideConcurrencyLimit)
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
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM daily_usage WHERE user_id = %s", s.placeholder(1)), id); err != nil {
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
	err := s.db.QueryRowContext(ctx, `SELECT architect_system_prompt, svg_system_prompt, COALESCE(theme_system_prompt, ''),
		architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model, COALESCE(architect_request_json, ''), COALESCE(architect_provider_id, ''), COALESCE(architect_model, ''),
		svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model, COALESCE(svg_request_json, ''), COALESCE(svg_provider_id, ''), COALESCE(svg_model, ''),
		COALESCE(theme_model_provider, ''), COALESCE(theme_model_api_key, ''), COALESCE(theme_model_base_url, ''), COALESCE(theme_model_model, ''), COALESCE(theme_request_json, ''), COALESCE(theme_provider_id, ''), COALESCE(theme_model, ''),
		COALESCE(architect_workflow_json, ''),
		updated_at, updated_by FROM prompt_settings WHERE id = 1`).Scan(
		&settings.Architect.SystemPrompt,
		&settings.SVG.SystemPrompt,
		&settings.Theme.SystemPrompt,
		&settings.Architect.ModelConfig.Provider,
		&settings.Architect.ModelConfig.APIKey,
		&settings.Architect.ModelConfig.BaseURL,
		&settings.Architect.ModelConfig.Model,
		&settings.Architect.RequestJSON,
		&settings.Architect.ProviderID,
		&settings.Architect.Model,
		&settings.SVG.ModelConfig.Provider,
		&settings.SVG.ModelConfig.APIKey,
		&settings.SVG.ModelConfig.BaseURL,
		&settings.SVG.ModelConfig.Model,
		&settings.SVG.RequestJSON,
		&settings.SVG.ProviderID,
		&settings.SVG.Model,
		&settings.Theme.ModelConfig.Provider,
		&settings.Theme.ModelConfig.APIKey,
		&settings.Theme.ModelConfig.BaseURL,
		&settings.Theme.ModelConfig.Model,
		&settings.Theme.RequestJSON,
		&settings.Theme.ProviderID,
		&settings.Theme.Model,
		&settings.ArchitectWorkflowJSON,
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
	if settings.Architect.ProviderID != "" && settings.Architect.Model != "" {
		settings.Architect.ModelConfig.Model = settings.Architect.Model
	}
	if settings.SVG.ProviderID != "" && settings.SVG.Model != "" {
		settings.SVG.ModelConfig.Model = settings.SVG.Model
	}
	if settings.Theme.ProviderID != "" && settings.Theme.Model != "" {
		settings.Theme.ModelConfig.Model = settings.Theme.Model
	}
	var err error
	if s.dialect == "postgres" {
		_, err = s.db.ExecContext(ctx, `INSERT INTO prompt_settings (
			id, architect_system_prompt, svg_system_prompt,
			architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model, architect_request_json,
			svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model, svg_request_json,
			updated_at, updated_by)
			VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (id) DO UPDATE SET
			architect_system_prompt = EXCLUDED.architect_system_prompt,
			svg_system_prompt = EXCLUDED.svg_system_prompt,
			architect_model_provider = EXCLUDED.architect_model_provider,
			architect_model_api_key = EXCLUDED.architect_model_api_key,
			architect_model_base_url = EXCLUDED.architect_model_base_url,
			architect_model_model = EXCLUDED.architect_model_model,
			architect_request_json = EXCLUDED.architect_request_json,
			svg_model_provider = EXCLUDED.svg_model_provider,
			svg_model_api_key = EXCLUDED.svg_model_api_key,
			svg_model_base_url = EXCLUDED.svg_model_base_url,
			svg_model_model = EXCLUDED.svg_model_model,
			svg_request_json = EXCLUDED.svg_request_json,
			updated_at = EXCLUDED.updated_at,
			updated_by = EXCLUDED.updated_by`,
			settings.Architect.SystemPrompt, settings.SVG.SystemPrompt,
			settings.Architect.ModelConfig.Provider, settings.Architect.ModelConfig.APIKey, settings.Architect.ModelConfig.BaseURL, settings.Architect.ModelConfig.Model, settings.Architect.RequestJSON,
			settings.SVG.ModelConfig.Provider, settings.SVG.ModelConfig.APIKey, settings.SVG.ModelConfig.BaseURL, settings.SVG.ModelConfig.Model, settings.SVG.RequestJSON,
			settings.UpdatedAt, settings.UpdatedBy)
	} else {
		_, err = s.db.ExecContext(ctx, `INSERT INTO prompt_settings (
			id, architect_system_prompt, svg_system_prompt,
			architect_model_provider, architect_model_api_key, architect_model_base_url, architect_model_model, architect_request_json,
			svg_model_provider, svg_model_api_key, svg_model_base_url, svg_model_model, svg_request_json,
			updated_at, updated_by)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			architect_system_prompt = excluded.architect_system_prompt,
			svg_system_prompt = excluded.svg_system_prompt,
			architect_model_provider = excluded.architect_model_provider,
			architect_model_api_key = excluded.architect_model_api_key,
			architect_model_base_url = excluded.architect_model_base_url,
			architect_model_model = excluded.architect_model_model,
			architect_request_json = excluded.architect_request_json,
			svg_model_provider = excluded.svg_model_provider,
			svg_model_api_key = excluded.svg_model_api_key,
			svg_model_base_url = excluded.svg_model_base_url,
			svg_model_model = excluded.svg_model_model,
			svg_request_json = excluded.svg_request_json,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`,
			settings.Architect.SystemPrompt, settings.SVG.SystemPrompt,
			settings.Architect.ModelConfig.Provider, settings.Architect.ModelConfig.APIKey, settings.Architect.ModelConfig.BaseURL, settings.Architect.ModelConfig.Model, settings.Architect.RequestJSON,
			settings.SVG.ModelConfig.Provider, settings.SVG.ModelConfig.APIKey, settings.SVG.ModelConfig.BaseURL, settings.SVG.ModelConfig.Model, settings.SVG.RequestJSON,
			settings.UpdatedAt, settings.UpdatedBy)
	}
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE prompt_settings SET
		architect_provider_id = %s, architect_model = %s,
		svg_provider_id = %s, svg_model = %s,
		theme_system_prompt = %s,
		theme_model_provider = %s, theme_model_api_key = %s, theme_model_base_url = %s, theme_model_model = %s, theme_request_json = %s,
		theme_provider_id = %s, theme_model = %s,
		architect_workflow_json = %s
		WHERE id = 1`,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5),
		s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9), s.placeholder(10),
		s.placeholder(11), s.placeholder(12), s.placeholder(13)),
		settings.Architect.ProviderID, settings.Architect.Model,
		settings.SVG.ProviderID, settings.SVG.Model,
		settings.Theme.SystemPrompt,
		settings.Theme.ModelConfig.Provider, settings.Theme.ModelConfig.APIKey, settings.Theme.ModelConfig.BaseURL, settings.Theme.ModelConfig.Model, settings.Theme.RequestJSON,
		settings.Theme.ProviderID, settings.Theme.Model,
		settings.ArchitectWorkflowJSON)
	return err
}

func (s *SQLStore) GetSystemSettings(ctx context.Context) (SystemSettings, error) {
	var settings SystemSettings
	err := s.db.QueryRowContext(ctx, `SELECT registration_enabled, default_user_group_id, default_slide_concurrency_limit, updated_at, updated_by FROM system_settings WHERE id = 1`).Scan(&settings.RegistrationEnabled, &settings.DefaultUserGroupID, &settings.DefaultSlideConcurrencyLimit, &settings.UpdatedAt, &settings.UpdatedBy)
	if err == sql.ErrNoRows {
		return DefaultSystemSettings(time.Now().UTC()), nil
	}
	if err != nil {
		return SystemSettings{}, err
	}
	return settings, nil
}

func (s *SQLStore) SaveSystemSettings(ctx context.Context, settings SystemSettings) error {
	if settings.DefaultUserGroupID == "" {
		settings.DefaultUserGroupID = DefaultUserGroupID
	}
	if _, err := s.GetUserGroup(ctx, settings.DefaultUserGroupID); err != nil {
		return err
	}
	if settings.DefaultSlideConcurrencyLimit <= 0 {
		settings.DefaultSlideConcurrencyLimit = DefaultSlideConcurrencyLimit
	}
	settings.UpdatedAt = time.Now().UTC()
	if s.dialect == "postgres" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO system_settings (id, registration_enabled, default_user_group_id, updated_at, updated_by)
			VALUES (1, $1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET registration_enabled = EXCLUDED.registration_enabled, default_user_group_id = EXCLUDED.default_user_group_id, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
			settings.RegistrationEnabled, settings.DefaultUserGroupID, settings.UpdatedAt, settings.UpdatedBy)
		if err != nil {
			return err
		}
	} else {
		_, err := s.db.ExecContext(ctx, `INSERT INTO system_settings (id, registration_enabled, default_user_group_id, updated_at, updated_by)
			VALUES (1, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET registration_enabled = excluded.registration_enabled, default_user_group_id = excluded.default_user_group_id, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			boolInt(settings.RegistrationEnabled), settings.DefaultUserGroupID, settings.UpdatedAt, settings.UpdatedBy)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, "UPDATE user_groups SET is_default = CASE WHEN id = "+s.placeholder(1)+" THEN 1 ELSE 0 END", settings.DefaultUserGroupID)
	return err
}

func (s *SQLStore) CreateUserGroup(ctx context.Context, input CreateUserGroupInput) (UserGroup, error) {
	now := time.Now().UTC()
	id := input.ID
	if id == "" {
		id = newID()
	}
	createdAt := now
	updatedAt := now
	if input.CreatedAt != nil {
		createdAt = *input.CreatedAt
	}
	if input.UpdatedAt != nil {
		updatedAt = *input.UpdatedAt
	}
	group := UserGroup{ID: id, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), DailyPPTLimit: input.DailyPPTLimit, DailySlideLimit: input.DailySlideLimit, SlideConcurrencyLimit: input.SlideConcurrencyLimit, IsDefault: input.IsDefault, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if group.SlideConcurrencyLimit <= 0 {
		settings, err := s.GetSystemSettings(ctx)
		if err != nil {
			return UserGroup{}, err
		}
		group.SlideConcurrencyLimit = settings.DefaultSlideConcurrencyLimit
	}
	if group.Name == "" || group.DailyPPTLimit < 0 || group.DailySlideLimit < 0 || group.SlideConcurrencyLimit < 1 {
		return UserGroup{}, ErrInvalidStore
	}
	query := fmt.Sprintf("INSERT INTO user_groups (id, name, description, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, is_default, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)", s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9))
	if _, err := s.db.ExecContext(ctx, query, group.ID, group.Name, group.Description, group.DailyPPTLimit, group.DailySlideLimit, group.SlideConcurrencyLimit, boolInt(group.IsDefault), group.CreatedAt, group.UpdatedAt); err != nil {
		return UserGroup{}, mapSQLErr(err)
	}
	if group.IsDefault {
		settings, err := s.GetSystemSettings(ctx)
		if err != nil {
			return UserGroup{}, err
		}
		settings.DefaultUserGroupID = group.ID
		if err := s.SaveSystemSettings(ctx, settings); err != nil {
			return UserGroup{}, err
		}
	}
	return group, nil
}

func (s *SQLStore) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, description, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, is_default, created_at, updated_at FROM user_groups ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []UserGroup
	for rows.Next() {
		group, err := scanUserGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *SQLStore) GetUserGroup(ctx context.Context, id string) (UserGroup, error) {
	query := fmt.Sprintf("SELECT id, name, description, daily_ppt_limit, daily_slide_limit, slide_concurrency_limit, is_default, created_at, updated_at FROM user_groups WHERE id = %s", s.placeholder(1))
	return scanUserGroup(s.db.QueryRowContext(ctx, query, id))
}

func (s *SQLStore) UpdateUserGroup(ctx context.Context, id string, input UpdateUserGroupInput) (UserGroup, error) {
	sets := []string{}
	args := []any{}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = %s", column, s.placeholder(len(args))))
	}
	if input.Name != nil {
		add("name", strings.TrimSpace(*input.Name))
	}
	if input.Description != nil {
		add("description", strings.TrimSpace(*input.Description))
	}
	if input.DailyPPTLimit != nil {
		if *input.DailyPPTLimit < 0 {
			return UserGroup{}, ErrInvalidStore
		}
		add("daily_ppt_limit", *input.DailyPPTLimit)
	}
	if input.DailySlideLimit != nil {
		if *input.DailySlideLimit < 0 {
			return UserGroup{}, ErrInvalidStore
		}
		add("daily_slide_limit", *input.DailySlideLimit)
	}
	if input.SlideConcurrencyLimit != nil {
		if *input.SlideConcurrencyLimit < 1 {
			return UserGroup{}, ErrInvalidStore
		}
		add("slide_concurrency_limit", *input.SlideConcurrencyLimit)
	}
	if input.IsDefault != nil {
		add("is_default", boolInt(*input.IsDefault))
	}
	add("updated_at", time.Now().UTC())
	args = append(args, id)
	query := fmt.Sprintf("UPDATE user_groups SET %s WHERE id = %s", strings.Join(sets, ", "), s.placeholder(len(args)))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return UserGroup{}, mapSQLErr(err)
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return UserGroup{}, ErrNotFound
	}
	if input.IsDefault != nil && *input.IsDefault {
		settings, err := s.GetSystemSettings(ctx)
		if err != nil {
			return UserGroup{}, err
		}
		settings.DefaultUserGroupID = id
		if err := s.SaveSystemSettings(ctx, settings); err != nil {
			return UserGroup{}, err
		}
	}
	return s.GetUserGroup(ctx, id)
}

func (s *SQLStore) DeleteUserGroup(ctx context.Context, id string) error {
	settings, err := s.GetSystemSettings(ctx)
	if err != nil {
		return err
	}
	if settings.DefaultUserGroupID == id {
		return ErrInvalidStore
	}
	var count int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM users WHERE group_id = %s", s.placeholder(1)), id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrInvalidStore
	}
	result, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM user_groups WHERE id = %s", s.placeholder(1)), id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) GetEffectiveQuota(ctx context.Context, userID string, date string) (EffectiveQuota, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return EffectiveQuota{}, err
	}
	return s.effectiveQuota(ctx, user, date)
}

func (s *SQLStore) ReserveDailyQuota(ctx context.Context, input ReserveQuotaInput) (QuotaReservation, error) {
	if input.Date == "" {
		input.Date = TodayUTC()
	}
	user, err := s.GetUserByID(ctx, input.UserID)
	if err != nil {
		return QuotaReservation{}, err
	}
	quota, err := s.effectiveQuota(ctx, user, input.Date)
	if err != nil {
		return QuotaReservation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return QuotaReservation{}, err
	}
	defer tx.Rollback()
	if err := s.ensureUsageTx(ctx, tx, input.UserID, input.Date); err != nil {
		return QuotaReservation{}, err
	}
	query := fmt.Sprintf(`UPDATE daily_usage SET ppt_reserved = ppt_reserved + %s, slides_reserved = slides_reserved + %s, updated_at = %s
		WHERE user_id = %s AND usage_date = %s AND slides_used + slides_reserved + %s <= %s`,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7))
	result, err := tx.ExecContext(ctx, query, input.PPTs, input.Slides, time.Now().UTC(), input.UserID, input.Date, input.Slides, quota.DailySlideLimit)
	if err != nil {
		return QuotaReservation{}, err
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return QuotaReservation{}, ErrQuotaExceeded
	}
	if err := tx.Commit(); err != nil {
		return QuotaReservation{}, err
	}
	return QuotaReservation{UserID: input.UserID, Date: input.Date, PPTs: input.PPTs, Slides: input.Slides}, nil
}

func (s *SQLStore) CommitDailyQuota(ctx context.Context, reservation QuotaReservation, actualSlides int) (EffectiveQuota, error) {
	if reservation.Date == "" {
		reservation.Date = TodayUTC()
	}
	now := time.Now().UTC()
	query := fmt.Sprintf(`UPDATE daily_usage SET ppt_reserved = CASE WHEN ppt_reserved - %s < 0 THEN 0 ELSE ppt_reserved - %s END,
		slides_reserved = CASE WHEN slides_reserved - %s < 0 THEN 0 ELSE slides_reserved - %s END,
		ppt_used = ppt_used + %s, slides_used = slides_used + %s, updated_at = %s WHERE user_id = %s AND usage_date = %s`,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9))
	if _, err := s.db.ExecContext(ctx, query, reservation.PPTs, reservation.PPTs, reservation.Slides, reservation.Slides, reservation.PPTs, actualSlides, now, reservation.UserID, reservation.Date); err != nil {
		return EffectiveQuota{}, err
	}
	return s.GetEffectiveQuota(ctx, reservation.UserID, reservation.Date)
}

func (s *SQLStore) ReleaseDailyQuota(ctx context.Context, reservation QuotaReservation) error {
	if reservation.Date == "" {
		reservation.Date = TodayUTC()
	}
	query := fmt.Sprintf(`UPDATE daily_usage SET ppt_reserved = CASE WHEN ppt_reserved - %s < 0 THEN 0 ELSE ppt_reserved - %s END,
		slides_reserved = CASE WHEN slides_reserved - %s < 0 THEN 0 ELSE slides_reserved - %s END, updated_at = %s WHERE user_id = %s AND usage_date = %s`,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7))
	_, err := s.db.ExecContext(ctx, query, reservation.PPTs, reservation.PPTs, reservation.Slides, reservation.Slides, time.Now().UTC(), reservation.UserID, reservation.Date)
	return err
}

func (s *SQLStore) ListDailyUsages(ctx context.Context) ([]DailyUsage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT user_id, usage_date, ppt_used, slides_used, ppt_reserved, slides_reserved, updated_at FROM daily_usage")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var usages []DailyUsage
	for rows.Next() {
		usage, err := scanDailyUsage(rows)
		if err != nil {
			return nil, err
		}
		usages = append(usages, usage)
	}
	return usages, rows.Err()
}

func (s *SQLStore) UpsertDailyUsage(ctx context.Context, usage DailyUsage) error {
	if usage.Date == "" {
		usage.Date = TodayUTC()
	}
	if usage.UpdatedAt.IsZero() {
		usage.UpdatedAt = time.Now().UTC()
	}
	if s.dialect == "postgres" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO daily_usage (user_id, usage_date, ppt_used, slides_used, ppt_reserved, slides_reserved, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (user_id, usage_date) DO UPDATE SET ppt_used = EXCLUDED.ppt_used, slides_used = EXCLUDED.slides_used, ppt_reserved = EXCLUDED.ppt_reserved, slides_reserved = EXCLUDED.slides_reserved, updated_at = EXCLUDED.updated_at`,
			usage.UserID, usage.Date, usage.PPTUsed, usage.SlidesUsed, usage.PPTReserved, usage.SlidesReserved, usage.UpdatedAt)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO daily_usage (user_id, usage_date, ppt_used, slides_used, ppt_reserved, slides_reserved, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(user_id, usage_date) DO UPDATE SET ppt_used = excluded.ppt_used, slides_used = excluded.slides_used, ppt_reserved = excluded.ppt_reserved, slides_reserved = excluded.slides_reserved, updated_at = excluded.updated_at`,
		usage.UserID, usage.Date, usage.PPTUsed, usage.SlidesUsed, usage.PPTReserved, usage.SlidesReserved, usage.UpdatedAt)
	return err
}

func (s *SQLStore) effectiveQuota(ctx context.Context, user User, date string) (EffectiveQuota, error) {
	if date == "" {
		date = TodayUTC()
	}
	group, err := s.GetUserGroup(ctx, user.GroupID)
	if err != nil {
		group, err = s.GetUserGroup(ctx, DefaultUserGroupID)
		if err != nil {
			return EffectiveQuota{}, err
		}
	}
	pptLimit, slideLimit, source := ResolveQuotaLimits(user, group)
	usage, err := s.getDailyUsage(ctx, user.ID, date)
	if err != nil && err != ErrNotFound {
		return EffectiveQuota{}, err
	}
	if err == ErrNotFound {
		usage = DailyUsage{UserID: user.ID, Date: date}
	}
	return BuildEffectiveQuota(date, pptLimit, slideLimit, source, group, usage), nil
}

func (s *SQLStore) getDailyUsage(ctx context.Context, userID, date string) (DailyUsage, error) {
	query := fmt.Sprintf("SELECT user_id, usage_date, ppt_used, slides_used, ppt_reserved, slides_reserved, updated_at FROM daily_usage WHERE user_id = %s AND usage_date = %s", s.placeholder(1), s.placeholder(2))
	return scanDailyUsage(s.db.QueryRowContext(ctx, query, userID, date))
}

func (s *SQLStore) ensureUsageTx(ctx context.Context, tx *sql.Tx, userID, date string) error {
	now := time.Now().UTC()
	if s.dialect == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO daily_usage (user_id, usage_date, updated_at) VALUES ($1, $2, $3) ON CONFLICT (user_id, usage_date) DO NOTHING`, userID, date, now)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_usage (user_id, usage_date, updated_at) VALUES (?, ?, ?) ON CONFLICT(user_id, usage_date) DO NOTHING`, userID, date, now)
	return err
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func (s *SQLStore) ListLLMProviders(ctx context.Context) ([]LLMProvider, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, kind, base_url, api_key, COALESCE(proxy, ''), enabled, created_at, updated_at FROM llm_providers ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMProvider
	for rows.Next() {
		var p LLMProvider
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Proxy, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetLLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	query := fmt.Sprintf("SELECT id, name, kind, base_url, api_key, COALESCE(proxy, ''), enabled, created_at, updated_at FROM llm_providers WHERE id = %s", s.placeholder(1))
	var p LLMProvider
	var enabled int
	err := s.db.QueryRowContext(ctx, query, id).Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Proxy, &enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return LLMProvider{}, mapSQLErr(err)
	}
	p.Enabled = enabled != 0
	return p, nil
}

func (s *SQLStore) CreateLLMProvider(ctx context.Context, input CreateLLMProviderInput) (LLMProvider, error) {
	now := time.Now().UTC()
	id := input.ID
	if id == "" {
		id = newID()
	}
	name := strings.TrimSpace(input.Name)
	kind := strings.TrimSpace(strings.ToLower(input.Kind))
	if name == "" || kind == "" || strings.TrimSpace(input.APIKey) == "" {
		return LLMProvider{}, ErrInvalidStore
	}
	p := LLMProvider{ID: id, Name: name, Kind: kind, BaseURL: strings.TrimSpace(input.BaseURL), APIKey: input.APIKey, Proxy: strings.TrimSpace(input.Proxy), Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now}
	query := fmt.Sprintf("INSERT INTO llm_providers (id, name, kind, base_url, api_key, proxy, enabled, created_at, updated_at) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s)",
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9))
	if _, err := s.db.ExecContext(ctx, query, p.ID, p.Name, p.Kind, p.BaseURL, p.APIKey, p.Proxy, boolInt(p.Enabled), p.CreatedAt, p.UpdatedAt); err != nil {
		return LLMProvider{}, mapSQLErr(err)
	}
	return p, nil
}

func (s *SQLStore) UpdateLLMProvider(ctx context.Context, id string, input UpdateLLMProviderInput) (LLMProvider, error) {
	sets := []string{}
	args := []any{}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = %s", column, s.placeholder(len(args))))
	}
	if input.Name != nil {
		add("name", strings.TrimSpace(*input.Name))
	}
	if input.Kind != nil {
		add("kind", strings.TrimSpace(strings.ToLower(*input.Kind)))
	}
	if input.BaseURL != nil {
		add("base_url", strings.TrimSpace(*input.BaseURL))
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		add("api_key", *input.APIKey)
	}
	if input.Proxy != nil {
		add("proxy", strings.TrimSpace(*input.Proxy))
	}
	if input.Enabled != nil {
		add("enabled", boolInt(*input.Enabled))
	}
	add("updated_at", time.Now().UTC())
	args = append(args, id)
	query := fmt.Sprintf("UPDATE llm_providers SET %s WHERE id = %s", strings.Join(sets, ", "), s.placeholder(len(args)))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return LLMProvider{}, mapSQLErr(err)
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return LLMProvider{}, ErrNotFound
	}
	return s.GetLLMProvider(ctx, id)
}

func (s *SQLStore) DeleteLLMProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM llm_providers WHERE id = %s", s.placeholder(1)), id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err == nil && count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) SaveGenerationJob(ctx context.Context, job GenerationJobRecord) error {
	payload := string(job.PayloadJSON)
	result := string(job.ResultJSON)
	if s.dialect == "postgres" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO generation_jobs (
			id, user_id, type, status, progress, error, parent_job_id, label, payload_json, result_json, created_at, updated_at, started_at, finished_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			type = EXCLUDED.type,
			status = EXCLUDED.status,
			progress = EXCLUDED.progress,
			error = EXCLUDED.error,
			parent_job_id = EXCLUDED.parent_job_id,
			label = EXCLUDED.label,
			payload_json = EXCLUDED.payload_json,
			result_json = EXCLUDED.result_json,
			updated_at = EXCLUDED.updated_at,
			started_at = EXCLUDED.started_at,
			finished_at = EXCLUDED.finished_at`,
			job.ID, job.UserID, job.Type, job.Status, job.Progress, job.Error, job.ParentJobID, job.Label, payload, result, job.CreatedAt, job.UpdatedAt, job.StartedAt, job.FinishedAt)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO generation_jobs (
		id, user_id, type, status, progress, error, parent_job_id, label, payload_json, result_json, created_at, updated_at, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		user_id = excluded.user_id,
		type = excluded.type,
		status = excluded.status,
		progress = excluded.progress,
		error = excluded.error,
		parent_job_id = excluded.parent_job_id,
		label = excluded.label,
		payload_json = excluded.payload_json,
		result_json = excluded.result_json,
		updated_at = excluded.updated_at,
		started_at = excluded.started_at,
		finished_at = excluded.finished_at`,
		job.ID, job.UserID, job.Type, job.Status, job.Progress, job.Error, job.ParentJobID, job.Label, payload, result, job.CreatedAt, job.UpdatedAt, job.StartedAt, job.FinishedAt)
	return err
}

func (s *SQLStore) GetGenerationJob(ctx context.Context, id string) (GenerationJobRecord, error) {
	query := fmt.Sprintf(`SELECT id, user_id, type, status, progress, error, COALESCE(parent_job_id,''), COALESCE(label,''), payload_json, result_json, created_at, updated_at, started_at, finished_at
		FROM generation_jobs WHERE id = %s`, s.placeholder(1))
	return scanGenerationJob(s.db.QueryRowContext(ctx, query, id))
}

func (s *SQLStore) ListGenerationJobsByUser(ctx context.Context, userID string, limit int) ([]GenerationJobRecord, error) {
	if limit <= 0 {
		limit = 30
	}
	query := fmt.Sprintf(`SELECT id, user_id, type, status, progress, error, COALESCE(parent_job_id,''), COALESCE(label,''), payload_json, result_json, created_at, updated_at, started_at, finished_at
		FROM generation_jobs WHERE user_id = %s AND COALESCE(parent_job_id,'') = '' ORDER BY created_at DESC LIMIT %s`, s.placeholder(1), s.placeholder(2))
	rows, err := s.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationJobRecord
	for rows.Next() {
		job, err := scanGenerationJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListOpenGenerationJobs(ctx context.Context, limit int) ([]GenerationJobRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	query := fmt.Sprintf(`SELECT id, user_id, type, status, progress, error, COALESCE(parent_job_id,''), COALESCE(label,''), payload_json, result_json, created_at, updated_at, started_at, finished_at
		FROM generation_jobs WHERE status IN (%s, %s) ORDER BY created_at ASC LIMIT %s`, s.placeholder(1), s.placeholder(2), s.placeholder(3))
	rows, err := s.db.QueryContext(ctx, query, "queued", "running", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationJobRecord
	for rows.Next() {
		job, err := scanGenerationJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *SQLStore) DeleteGenerationJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM generation_jobs WHERE id = %s", s.placeholder(1)), id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGenerationJob(row rowScanner) (GenerationJobRecord, error) {
	var job GenerationJobRecord
	var payload, result string
	var started, finished *time.Time
	err := row.Scan(&job.ID, &job.UserID, &job.Type, &job.Status, &job.Progress, &job.Error, &job.ParentJobID, &job.Label, &payload, &result, &job.CreatedAt, &job.UpdatedAt, &started, &finished)
	if err != nil {
		return GenerationJobRecord{}, mapSQLErr(err)
	}
	if payload != "" {
		job.PayloadJSON = json.RawMessage(payload)
	}
	if result != "" {
		job.ResultJSON = json.RawMessage(result)
	}
	job.StartedAt = started
	job.FinishedAt = finished
	return job, nil
}

func (s *SQLStore) CreateUpload(ctx context.Context, up Upload) (Upload, error) {
	if up.ID == "" {
		up.ID = newID()
	}
	if up.CreatedAt.IsZero() {
		up.CreatedAt = time.Now().UTC()
	}
	query := fmt.Sprintf(`INSERT INTO uploads (id, user_id, filename, content_type, size_bytes, path, created_at)
		VALUES (%s,%s,%s,%s,%s,%s,%s)`, s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7))
	if _, err := s.db.ExecContext(ctx, query, up.ID, up.UserID, up.Filename, up.ContentType, up.SizeBytes, up.Path, up.CreatedAt); err != nil {
		return Upload{}, mapSQLErr(err)
	}
	return up, nil
}

func (s *SQLStore) GetUpload(ctx context.Context, id string) (Upload, error) {
	query := fmt.Sprintf(`SELECT id, user_id, filename, content_type, size_bytes, path, created_at FROM uploads WHERE id = %s`, s.placeholder(1))
	var up Upload
	err := s.db.QueryRowContext(ctx, query, id).Scan(&up.ID, &up.UserID, &up.Filename, &up.ContentType, &up.SizeBytes, &up.Path, &up.CreatedAt)
	if err != nil {
		return Upload{}, mapSQLErr(err)
	}
	return up, nil
}

func (s *SQLStore) ListUploadsByUser(ctx context.Context, userID string, limit int) ([]Upload, error) {
	if limit <= 0 {
		limit = 50
	}
	query := fmt.Sprintf(`SELECT id, user_id, filename, content_type, size_bytes, path, created_at FROM uploads WHERE user_id = %s ORDER BY created_at DESC LIMIT %s`, s.placeholder(1), s.placeholder(2))
	rows, err := s.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		var up Upload
		if err := rows.Scan(&up.ID, &up.UserID, &up.Filename, &up.ContentType, &up.SizeBytes, &up.Path, &up.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, up)
	}
	return out, rows.Err()
}

func (s *SQLStore) DeleteUpload(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM uploads WHERE id = %s", s.placeholder(1)), id)
	return err
}

func (s *SQLStore) ListChildGenerationJobs(ctx context.Context, parentJobID string, limit int) ([]GenerationJobRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := fmt.Sprintf(`SELECT id, user_id, type, status, progress, error, COALESCE(parent_job_id,''), COALESCE(label,''), payload_json, result_json, created_at, updated_at, started_at, finished_at
		FROM generation_jobs WHERE parent_job_id = %s ORDER BY created_at ASC LIMIT %s`, s.placeholder(1), s.placeholder(2))
	rows, err := s.db.QueryContext(ctx, query, parentJobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationJobRecord
	for rows.Next() {
		job, err := scanGenerationJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}
