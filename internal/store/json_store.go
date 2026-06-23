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
	Users          []storedUser   `json:"users"`
	Sessions       []Session      `json:"sessions"`
	PromptSettings PromptSettings `json:"promptSettings"`
}

func NewJSONStore(path string) (*JSONStore, error) {
	if path == "" {
		path = "data/app.json"
	}
	store := &JSONStore{path: path}
	if err := store.load(); err != nil {
		return nil, err
	}
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
	email := normalizeEmail(input.Email)
	for _, user := range s.data.Users {
		if normalizeEmail(user.Email) == email {
			return User{}, ErrAlreadyExists
		}
	}
	now := time.Now().UTC()
	role := input.Role
	if role == "" {
		role = RoleUser
	}
	user := storedUser{
		ID:           newID(),
		Email:        email,
		PasswordHash: input.PasswordHash,
		Role:         role,
		Disabled:     input.Disabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
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
		if input.PasswordHash != nil {
			s.data.Users[i].PasswordHash = *input.PasswordHash
		}
		if input.Role != nil {
			s.data.Users[i].Role = *input.Role
		}
		if input.Disabled != nil {
			s.data.Users[i].Disabled = *input.Disabled
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
