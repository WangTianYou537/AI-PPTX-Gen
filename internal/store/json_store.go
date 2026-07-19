package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type JSONStore struct {
	path string
	mu   sync.Mutex
	data jsonData
}

type jsonData struct {
	Users          []storedUser          `json:"users"`
	Sessions       []Session             `json:"sessions"`
	PromptSettings PromptSettings        `json:"promptSettings"`
	SystemSettings SystemSettings        `json:"systemSettings"`
	UserGroups     []UserGroup           `json:"userGroups"`
	DailyUsages    []DailyUsage          `json:"dailyUsages"`
	LLMProviders   []LLMProvider         `json:"llmProviders"`
	GenerationJobs []GenerationJobRecord `json:"generationJobs"`
	Uploads        []Upload              `json:"uploads"`
}

func NewJSONStore(path string) (*JSONStore, error) {
	if path == "" {
		path = "data/app.json"
	}
	store := &JSONStore{path: path}
	if err := store.load(); err != nil {
		return nil, err
	}
	store.normalizeLoadedDataLocked()
	return store, nil
}

func (s *JSONStore) Close() error { return nil }

func (s *JSONStore) load() error {
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	return json.Unmarshal(content, &s.data)
}

func (s *JSONStore) normalizeLoadedDataLocked() {
	now := time.Now().UTC()
	if len(s.data.UserGroups) == 0 {
		s.data.UserGroups = []UserGroup{DefaultUserGroup(now)}
	}
	defaultID := s.data.SystemSettings.DefaultUserGroupID
	if defaultID == "" {
		defaultID = DefaultUserGroupID
	}
	defaultFound := false
	for i := range s.data.UserGroups {
		if s.data.UserGroups[i].ID == "" {
			s.data.UserGroups[i].ID = newID()
		}
		if s.data.UserGroups[i].ID == defaultID || s.data.UserGroups[i].IsDefault {
			defaultID = s.data.UserGroups[i].ID
			defaultFound = true
		}
	}
	if !defaultFound {
		group := DefaultUserGroup(now)
		defaultID = group.ID
		s.data.UserGroups = append(s.data.UserGroups, group)
	}
	for i := range s.data.UserGroups {
		s.data.UserGroups[i].IsDefault = s.data.UserGroups[i].ID == defaultID
		if s.data.UserGroups[i].CreatedAt.IsZero() {
			s.data.UserGroups[i].CreatedAt = now
		}
		if s.data.UserGroups[i].UpdatedAt.IsZero() {
			s.data.UserGroups[i].UpdatedAt = now
		}
	}
	if s.data.SystemSettings.UpdatedAt.IsZero() {
		s.data.SystemSettings = DefaultSystemSettings(now)
	}
	if s.data.SystemSettings.DefaultSlideConcurrencyLimit <= 0 {
		s.data.SystemSettings.DefaultSlideConcurrencyLimit = DefaultSlideConcurrencyLimit
	}
	s.data.SystemSettings.DefaultUserGroupID = defaultID
	for i := range s.data.UserGroups {
		if s.data.UserGroups[i].SlideConcurrencyLimit <= 0 {
			s.data.UserGroups[i].SlideConcurrencyLimit = s.data.SystemSettings.DefaultSlideConcurrencyLimit
		}
	}
	for i := range s.data.Users {
		if s.data.Users[i].Username == "" {
			s.data.Users[i].Username = DefaultUsername(s.data.Users[i].Email)
		}
		if s.data.Users[i].GroupID == "" {
			s.data.Users[i].GroupID = defaultID
		}
	}
}

func (s *JSONStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *JSONStore) NeedsSetup(ctx context.Context) (bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.data.Users {
		if user.Role == RoleAdmin {
			return false, nil
		}
	}
	return true, nil
}

func (s *JSONStore) CreateUser(ctx context.Context, input CreateUserInput) (User, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	email := normalizeEmail(input.Email)
	for _, user := range s.data.Users {
		if normalizeEmail(user.Email) == email {
			return User{}, ErrAlreadyExists
		}
	}
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
	groupID := input.GroupID
	if groupID == "" {
		groupID = s.data.SystemSettings.DefaultUserGroupID
	}
	if _, ok := s.findGroupLocked(groupID); !ok {
		return User{}, ErrNotFound
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = DefaultUsername(email)
	}
	user := storedUser{ID: id, Email: email, Username: username, PasswordHash: input.PasswordHash, Role: role, Disabled: input.Disabled, GroupID: groupID, DailyPPTLimit: cloneIntPtr(input.DailyPPTLimit), DailySlideLimit: cloneIntPtr(input.DailySlideLimit), SlideConcurrencyLimit: cloneIntPtr(input.SlideConcurrencyLimit), CreatedAt: createdAt, UpdatedAt: updatedAt}
	s.data.Users = append(s.data.Users, user)
	if err := s.saveLocked(); err != nil {
		return User{}, err
	}
	return publicUser(user), nil
}

func (s *JSONStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	email = normalizeEmail(email)
	for _, user := range s.data.Users {
		if normalizeEmail(user.Email) == email {
			return publicUser(user), nil
		}
	}
	return User{}, ErrNotFound
}

func (s *JSONStore) GetUserByID(ctx context.Context, id string) (User, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	for _, user := range s.data.Users {
		if user.ID == id {
			return publicUser(user), nil
		}
	}
	return User{}, ErrNotFound
}

func (s *JSONStore) ListUsers(ctx context.Context) ([]User, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	users := make([]User, 0, len(s.data.Users))
	for _, user := range s.data.Users {
		users = append(users, publicUser(user))
	}
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	return users, nil
}

func (s *JSONStore) UpdateUser(ctx context.Context, id string, input UpdateUserInput) (User, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	for i := range s.data.Users {
		if s.data.Users[i].ID != id {
			continue
		}
		if input.Email != nil {
			email := normalizeEmail(*input.Email)
			for _, user := range s.data.Users {
				if user.ID != id && normalizeEmail(user.Email) == email {
					return User{}, ErrAlreadyExists
				}
			}
			s.data.Users[i].Email = email
		}
		if input.Username != nil {
			s.data.Users[i].Username = strings.TrimSpace(*input.Username)
			if s.data.Users[i].Username == "" {
				s.data.Users[i].Username = DefaultUsername(s.data.Users[i].Email)
			}
		}
		if input.PasswordHash != nil {
			s.data.Users[i].PasswordHash = *input.PasswordHash
		}
		if input.Role != nil {
			s.data.Users[i].Role = *input.Role
		}
		if input.Disabled != nil {
			s.data.Users[i].Disabled = *input.Disabled
		}
		if input.GroupID != nil {
			if _, ok := s.findGroupLocked(*input.GroupID); !ok {
				return User{}, ErrNotFound
			}
			s.data.Users[i].GroupID = *input.GroupID
		}
		if input.ClearDailyPPTLimit {
			s.data.Users[i].DailyPPTLimit = nil
		} else if input.DailyPPTLimit != nil {
			s.data.Users[i].DailyPPTLimit = cloneIntPtr(input.DailyPPTLimit)
		}
		if input.ClearDailySlideLimit {
			s.data.Users[i].DailySlideLimit = nil
		} else if input.DailySlideLimit != nil {
			s.data.Users[i].DailySlideLimit = cloneIntPtr(input.DailySlideLimit)
		}
		if input.ClearSlideConcurrencyLimit {
			s.data.Users[i].SlideConcurrencyLimit = nil
		} else if input.SlideConcurrencyLimit != nil {
			s.data.Users[i].SlideConcurrencyLimit = cloneIntPtr(input.SlideConcurrencyLimit)
		}
		s.data.Users[i].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(); err != nil {
			return User{}, err
		}
		return publicUser(s.data.Users[i]), nil
	}
	return User{}, ErrNotFound
}

func (s *JSONStore) DeleteUser(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, user := range s.data.Users {
		if user.ID == id {
			s.data.Users = append(s.data.Users[:i], s.data.Users[i+1:]...)
			filtered := s.data.Sessions[:0]
			for _, session := range s.data.Sessions {
				if session.UserID != id {
					filtered = append(filtered, session)
				}
			}
			s.data.Sessions = filtered
			usage := s.data.DailyUsages[:0]
			for _, entry := range s.data.DailyUsages {
				if entry.UserID != id {
					usage = append(usage, entry)
				}
			}
			s.data.DailyUsages = usage
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *JSONStore) CountAdmins(ctx context.Context) (int, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, user := range s.data.Users {
		if user.Role == RoleAdmin && !user.Disabled {
			count++
		}
	}
	return count, nil
}

func (s *JSONStore) CreateSession(ctx context.Context, session Session) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sessions = append(s.data.Sessions, session)
	return s.saveLocked()
}

func (s *JSONStore) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, session := range s.data.Sessions {
		if session.TokenHash == tokenHash {
			if session.ExpiresAt.Before(now) {
				return Session{}, ErrNotFound
			}
			return session, nil
		}
	}
	return Session{}, ErrNotFound
}

func (s *JSONStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, session := range s.data.Sessions {
		if session.TokenHash == tokenHash {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *JSONStore) GetPromptSettings(ctx context.Context) (PromptSettings, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.PromptSettings, nil
}

func (s *JSONStore) SavePromptSettings(ctx context.Context, settings PromptSettings) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	settings.UpdatedAt = time.Now().UTC()
	s.data.PromptSettings = settings
	return s.saveLocked()
}

func (s *JSONStore) GetSystemSettings(ctx context.Context) (SystemSettings, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	return s.data.SystemSettings, nil
}

func (s *JSONStore) SaveSystemSettings(ctx context.Context, settings SystemSettings) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	if settings.DefaultUserGroupID == "" {
		settings.DefaultUserGroupID = s.data.SystemSettings.DefaultUserGroupID
	}
	if _, ok := s.findGroupLocked(settings.DefaultUserGroupID); !ok {
		return ErrNotFound
	}
	if settings.DefaultSlideConcurrencyLimit <= 0 {
		settings.DefaultSlideConcurrencyLimit = DefaultSlideConcurrencyLimit
	}
	settings.UpdatedAt = time.Now().UTC()
	s.data.SystemSettings = settings
	s.setDefaultGroupLocked(settings.DefaultUserGroupID)
	return s.saveLocked()
}

func (s *JSONStore) CreateUserGroup(ctx context.Context, input CreateUserGroupInput) (UserGroup, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
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
		group.SlideConcurrencyLimit = s.data.SystemSettings.DefaultSlideConcurrencyLimit
	}
	if group.Name == "" {
		return UserGroup{}, ErrInvalidStore
	}
	if group.DailyPPTLimit < 0 || group.DailySlideLimit < 0 || group.SlideConcurrencyLimit < 1 {
		return UserGroup{}, ErrInvalidStore
	}
	s.data.UserGroups = append(s.data.UserGroups, group)
	if group.IsDefault {
		s.data.SystemSettings.DefaultUserGroupID = group.ID
		s.setDefaultGroupLocked(group.ID)
	}
	if err := s.saveLocked(); err != nil {
		return UserGroup{}, err
	}
	return group, nil
}

func (s *JSONStore) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	groups := append([]UserGroup(nil), s.data.UserGroups...)
	sort.Slice(groups, func(i, j int) bool { return groups[i].CreatedAt.Before(groups[j].CreatedAt) })
	return groups, nil
}

func (s *JSONStore) GetUserGroup(ctx context.Context, id string) (UserGroup, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	group, ok := s.findGroupLocked(id)
	if !ok {
		return UserGroup{}, ErrNotFound
	}
	return group, nil
}

func (s *JSONStore) UpdateUserGroup(ctx context.Context, id string, input UpdateUserGroupInput) (UserGroup, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	for i := range s.data.UserGroups {
		if s.data.UserGroups[i].ID != id {
			continue
		}
		if input.Name != nil {
			s.data.UserGroups[i].Name = strings.TrimSpace(*input.Name)
		}
		if input.Description != nil {
			s.data.UserGroups[i].Description = strings.TrimSpace(*input.Description)
		}
		if input.DailyPPTLimit != nil {
			if *input.DailyPPTLimit < 0 {
				return UserGroup{}, ErrInvalidStore
			}
			s.data.UserGroups[i].DailyPPTLimit = *input.DailyPPTLimit
		}
		if input.DailySlideLimit != nil {
			if *input.DailySlideLimit < 0 {
				return UserGroup{}, ErrInvalidStore
			}
			s.data.UserGroups[i].DailySlideLimit = *input.DailySlideLimit
		}
		if input.SlideConcurrencyLimit != nil {
			if *input.SlideConcurrencyLimit < 1 {
				return UserGroup{}, ErrInvalidStore
			}
			s.data.UserGroups[i].SlideConcurrencyLimit = *input.SlideConcurrencyLimit
		}
		if input.IsDefault != nil && *input.IsDefault {
			s.data.SystemSettings.DefaultUserGroupID = id
			s.setDefaultGroupLocked(id)
		}
		s.data.UserGroups[i].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(); err != nil {
			return UserGroup{}, err
		}
		return s.data.UserGroups[i], nil
	}
	return UserGroup{}, ErrNotFound
}

func (s *JSONStore) DeleteUserGroup(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	if id == s.data.SystemSettings.DefaultUserGroupID {
		return ErrInvalidStore
	}
	for _, user := range s.data.Users {
		if user.GroupID == id {
			return ErrInvalidStore
		}
	}
	for i, group := range s.data.UserGroups {
		if group.ID == id {
			s.data.UserGroups = append(s.data.UserGroups[:i], s.data.UserGroups[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *JSONStore) GetEffectiveQuota(ctx context.Context, userID string, date string) (EffectiveQuota, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	return s.effectiveQuotaLocked(userID, date)
}

func (s *JSONStore) ReserveDailyQuota(ctx context.Context, input ReserveQuotaInput) (QuotaReservation, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	quota, err := s.effectiveQuotaLocked(input.UserID, input.Date)
	if err != nil {
		return QuotaReservation{}, err
	}
	if quota.SlidesRemaining < input.Slides {
		return QuotaReservation{}, ErrQuotaExceeded
	}
	usage := s.ensureUsageLocked(input.UserID, input.Date)
	usage.PPTReserved += input.PPTs
	usage.SlidesReserved += input.Slides
	usage.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(); err != nil {
		return QuotaReservation{}, err
	}
	return QuotaReservation{UserID: input.UserID, Date: input.Date, PPTs: input.PPTs, Slides: input.Slides}, nil
}

func (s *JSONStore) CommitDailyQuota(ctx context.Context, reservation QuotaReservation, actualSlides int) (EffectiveQuota, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLoadedDataLocked()
	usage := s.ensureUsageLocked(reservation.UserID, reservation.Date)
	usage.PPTReserved = maxInt(0, usage.PPTReserved-reservation.PPTs)
	usage.SlidesReserved = maxInt(0, usage.SlidesReserved-reservation.Slides)
	usage.PPTUsed += reservation.PPTs
	usage.SlidesUsed += actualSlides
	usage.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(); err != nil {
		return EffectiveQuota{}, err
	}
	return s.effectiveQuotaLocked(reservation.UserID, reservation.Date)
}

func (s *JSONStore) ReleaseDailyQuota(ctx context.Context, reservation QuotaReservation) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	usage := s.ensureUsageLocked(reservation.UserID, reservation.Date)
	usage.PPTReserved = maxInt(0, usage.PPTReserved-reservation.PPTs)
	usage.SlidesReserved = maxInt(0, usage.SlidesReserved-reservation.Slides)
	usage.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

func (s *JSONStore) ListDailyUsages(ctx context.Context) ([]DailyUsage, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DailyUsage(nil), s.data.DailyUsages...), nil
}

func (s *JSONStore) UpsertDailyUsage(ctx context.Context, usage DailyUsage) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.DailyUsages {
		if s.data.DailyUsages[i].UserID == usage.UserID && s.data.DailyUsages[i].Date == usage.Date {
			s.data.DailyUsages[i] = usage
			return s.saveLocked()
		}
	}
	s.data.DailyUsages = append(s.data.DailyUsages, usage)
	return s.saveLocked()
}

func (s *JSONStore) findGroupLocked(id string) (UserGroup, bool) {
	for _, group := range s.data.UserGroups {
		if group.ID == id {
			return group, true
		}
	}
	return UserGroup{}, false
}

func (s *JSONStore) setDefaultGroupLocked(id string) {
	for i := range s.data.UserGroups {
		s.data.UserGroups[i].IsDefault = s.data.UserGroups[i].ID == id
	}
	s.data.SystemSettings.DefaultUserGroupID = id
}

func (s *JSONStore) ensureUsageLocked(userID, date string) *DailyUsage {
	if date == "" {
		date = TodayUTC()
	}
	for i := range s.data.DailyUsages {
		if s.data.DailyUsages[i].UserID == userID && s.data.DailyUsages[i].Date == date {
			return &s.data.DailyUsages[i]
		}
	}
	s.data.DailyUsages = append(s.data.DailyUsages, DailyUsage{UserID: userID, Date: date, UpdatedAt: time.Now().UTC()})
	return &s.data.DailyUsages[len(s.data.DailyUsages)-1]
}

func (s *JSONStore) effectiveQuotaLocked(userID, date string) (EffectiveQuota, error) {
	if date == "" {
		date = TodayUTC()
	}
	var user storedUser
	found := false
	for _, entry := range s.data.Users {
		if entry.ID == userID {
			user = entry
			found = true
			break
		}
	}
	if !found {
		return EffectiveQuota{}, ErrNotFound
	}
	group, ok := s.findGroupLocked(user.GroupID)
	if !ok {
		group, _ = s.findGroupLocked(s.data.SystemSettings.DefaultUserGroupID)
	}
	pptLimit, slideLimit, source := ResolveQuotaLimits(publicUser(user), group)
	usage := s.ensureUsageLocked(userID, date)
	return BuildEffectiveQuota(date, pptLimit, slideLimit, source, group, *usage), nil
}

func (s *JSONStore) ListLLMProviders(ctx context.Context) ([]LLMProvider, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]LLMProvider(nil), s.data.LLMProviders...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *JSONStore) GetLLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.data.LLMProviders {
		if p.ID == id {
			return p, nil
		}
	}
	return LLMProvider{}, ErrNotFound
}

func (s *JSONStore) CreateLLMProvider(ctx context.Context, input CreateLLMProviderInput) (LLMProvider, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
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
	provider := LLMProvider{ID: id, Name: name, Kind: kind, BaseURL: strings.TrimSpace(input.BaseURL), APIKey: input.APIKey, Proxy: strings.TrimSpace(input.Proxy), Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now}
	s.data.LLMProviders = append(s.data.LLMProviders, provider)
	if err := s.saveLocked(); err != nil {
		return LLMProvider{}, err
	}
	return provider, nil
}

func (s *JSONStore) UpdateLLMProvider(ctx context.Context, id string, input UpdateLLMProviderInput) (LLMProvider, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.LLMProviders {
		if s.data.LLMProviders[i].ID != id {
			continue
		}
		if input.Name != nil {
			s.data.LLMProviders[i].Name = strings.TrimSpace(*input.Name)
		}
		if input.Kind != nil {
			s.data.LLMProviders[i].Kind = strings.TrimSpace(strings.ToLower(*input.Kind))
		}
		if input.BaseURL != nil {
			s.data.LLMProviders[i].BaseURL = strings.TrimSpace(*input.BaseURL)
		}
		if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
			s.data.LLMProviders[i].APIKey = *input.APIKey
		}
		if input.Proxy != nil {
			s.data.LLMProviders[i].Proxy = strings.TrimSpace(*input.Proxy)
		}
		if input.Enabled != nil {
			s.data.LLMProviders[i].Enabled = *input.Enabled
		}
		s.data.LLMProviders[i].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(); err != nil {
			return LLMProvider{}, err
		}
		return s.data.LLMProviders[i], nil
	}
	return LLMProvider{}, ErrNotFound
}

func (s *JSONStore) DeleteLLMProvider(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data.LLMProviders {
		if p.ID == id {
			s.data.LLMProviders = append(s.data.LLMProviders[:i], s.data.LLMProviders[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *JSONStore) SaveGenerationJob(ctx context.Context, job GenerationJobRecord) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.GenerationJobs {
		if s.data.GenerationJobs[i].ID == job.ID {
			s.data.GenerationJobs[i] = job
			return s.saveLocked()
		}
	}
	s.data.GenerationJobs = append(s.data.GenerationJobs, job)
	return s.saveLocked()
}

func (s *JSONStore) GetGenerationJob(ctx context.Context, id string) (GenerationJobRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.data.GenerationJobs {
		if job.ID == id {
			return job, nil
		}
	}
	return GenerationJobRecord{}, ErrNotFound
}

func (s *JSONStore) ListGenerationJobsByUser(ctx context.Context, userID string, limit int) ([]GenerationJobRecord, error) {
	_ = ctx
	if limit <= 0 {
		limit = 30
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GenerationJobRecord, 0, limit)
	for i := len(s.data.GenerationJobs) - 1; i >= 0; i-- {
		job := s.data.GenerationJobs[i]
		if job.UserID != userID {
			continue
		}
		// Hide child/retry tasks from the main list.
		if strings.TrimSpace(job.ParentJobID) != "" {
			continue
		}
		out = append(out, job)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *JSONStore) ListChildGenerationJobs(ctx context.Context, parentJobID string, limit int) ([]GenerationJobRecord, error) {
	_ = ctx
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GenerationJobRecord, 0)
	for _, job := range s.data.GenerationJobs {
		if job.ParentJobID != parentJobID {
			continue
		}
		out = append(out, job)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *JSONStore) ListOpenGenerationJobs(ctx context.Context, limit int) ([]GenerationJobRecord, error) {
	_ = ctx
	if limit <= 0 {
		limit = 200
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GenerationJobRecord, 0)
	for _, job := range s.data.GenerationJobs {
		if job.Status == "queued" || job.Status == "running" {
			out = append(out, job)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *JSONStore) DeleteGenerationJob(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, job := range s.data.GenerationJobs {
		if job.ID == id {
			s.data.GenerationJobs = append(s.data.GenerationJobs[:i], s.data.GenerationJobs[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *JSONStore) CreateUpload(ctx context.Context, up Upload) (Upload, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if up.ID == "" {
		up.ID = newID()
	}
	if up.CreatedAt.IsZero() {
		up.CreatedAt = time.Now().UTC()
	}
	s.data.Uploads = append(s.data.Uploads, up)
	if err := s.saveLocked(); err != nil {
		return Upload{}, err
	}
	return up, nil
}

func (s *JSONStore) GetUpload(ctx context.Context, id string) (Upload, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, up := range s.data.Uploads {
		if up.ID == id {
			return up, nil
		}
	}
	return Upload{}, ErrNotFound
}

func (s *JSONStore) ListUploadsByUser(ctx context.Context, userID string, limit int) ([]Upload, error) {
	_ = ctx
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Upload, 0, limit)
	for i := len(s.data.Uploads) - 1; i >= 0; i-- {
		up := s.data.Uploads[i]
		if up.UserID != userID {
			continue
		}
		out = append(out, up)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *JSONStore) DeleteUpload(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, up := range s.data.Uploads {
		if up.ID == id {
			s.data.Uploads = append(s.data.Uploads[:i], s.data.Uploads[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func TodayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
