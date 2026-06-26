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
	user := User{ID: id, Email: email, Username: username, PasswordHash: input.PasswordHash, Role: role, Disabled: input.Disabled, GroupID: groupID, DailyPPTLimit: cloneIntPtr(input.DailyPPTLimit), DailySlideLimit: cloneIntPtr(input.DailySlideLimit), CreatedAt: createdAt, UpdatedAt: updatedAt}
	query := fmt.Sprintf(
		"INSERT INTO users (id, email, username, password_hash, role, disabled, group_id, daily_ppt_limit, daily_slide_limit, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)",
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8), s.placeholder(9), s.placeholder(10), s.placeholder(11),
	)
	_, err := s.db.ExecContext(ctx, query, user.ID, user.Email, user.Username, user.PasswordHash, user.Role, boolInt(user.Disabled), user.GroupID, nullableInt(user.DailyPPTLimit), nullableInt(user.DailySlideLimit), user.CreatedAt, user.UpdatedAt)
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

func (s *SQLStore) GetSystemSettings(ctx context.Context) (SystemSettings, error) {
	var settings SystemSettings
	err := s.db.QueryRowContext(ctx, `SELECT registration_enabled, default_user_group_id, updated_at, updated_by FROM system_settings WHERE id = 1`).Scan(&settings.RegistrationEnabled, &settings.DefaultUserGroupID, &settings.UpdatedAt, &settings.UpdatedBy)
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
	group := UserGroup{ID: id, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), DailyPPTLimit: input.DailyPPTLimit, DailySlideLimit: input.DailySlideLimit, IsDefault: input.IsDefault, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if group.Name == "" || group.DailyPPTLimit < 0 || group.DailySlideLimit < 0 {
		return UserGroup{}, ErrInvalidStore
	}
	query := fmt.Sprintf("INSERT INTO user_groups (id, name, description, daily_ppt_limit, daily_slide_limit, is_default, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)", s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7), s.placeholder(8))
	if _, err := s.db.ExecContext(ctx, query, group.ID, group.Name, group.Description, group.DailyPPTLimit, group.DailySlideLimit, boolInt(group.IsDefault), group.CreatedAt, group.UpdatedAt); err != nil {
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
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, description, daily_ppt_limit, daily_slide_limit, is_default, created_at, updated_at FROM user_groups ORDER BY created_at ASC")
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
	query := fmt.Sprintf("SELECT id, name, description, daily_ppt_limit, daily_slide_limit, is_default, created_at, updated_at FROM user_groups WHERE id = %s", s.placeholder(1))
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
	pptLimit := group.DailyPPTLimit
	slideLimit := group.DailySlideLimit
	source := QuotaSourceGroup
	if user.DailyPPTLimit != nil {
		pptLimit = *user.DailyPPTLimit
		source = QuotaSourceUser
	}
	if user.DailySlideLimit != nil {
		slideLimit = *user.DailySlideLimit
		source = QuotaSourceUser
	}
	usage, err := s.getDailyUsage(ctx, user.ID, date)
	if err != nil && err != ErrNotFound {
		return EffectiveQuota{}, err
	}
	if err == ErrNotFound {
		usage = DailyUsage{UserID: user.ID, Date: date}
	}
	return buildEffectiveQuota(date, pptLimit, slideLimit, source, group, usage), nil
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
