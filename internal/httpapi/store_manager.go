package httpapi

import (
	"context"
	"sync"

	"wty5.cn/ppt-gen/internal/store"
)

type StoreManager struct {
	mu         sync.RWMutex
	store      store.Store
	config     store.Config
	configPath string
}

func NewStoreManager(active store.Store, cfg store.Config, configPath string) *StoreManager {
	return &StoreManager{store: active, config: cfg, configPath: configPath}
}

func (m *StoreManager) Current() (store.Store, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store, m.store != nil
}

func (m *StoreManager) Config() (store.Config, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return store.RedactConfig(m.config), m.store != nil
}

func (m *StoreManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return nil
	}
	return m.store.Close()
}

func (m *StoreManager) TestConfig(ctx context.Context, cfg store.Config) error {
	candidate, err := store.OpenConfiguredStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer candidate.Close()
	_, err = candidate.NeedsSetup(ctx)
	return err
}

func (m *StoreManager) ConfigureInitial(ctx context.Context, cfg store.Config) error {
	cfg = store.NormalizeConfig(cfg)
	candidate, err := store.OpenConfiguredStore(ctx, cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		_ = candidate.Close()
		return nil
	}
	if err := store.SaveConfig(m.configPath, cfg); err != nil {
		_ = candidate.Close()
		return err
	}
	m.store = candidate
	m.config = cfg
	return nil
}

func (m *StoreManager) SwitchStore(ctx context.Context, cfg store.Config) error {
	cfg = store.NormalizeConfig(cfg)
	candidate, err := store.OpenConfiguredStore(ctx, cfg)
	if err != nil {
		return err
	}

	m.mu.Lock()
	current := m.store
	if current == nil {
		m.mu.Unlock()
		_ = candidate.Close()
		return store.ErrInvalidStore
	}
	snapshot, err := store.ExportSnapshot(ctx, current)
	if err != nil {
		m.mu.Unlock()
		_ = candidate.Close()
		return err
	}
	if err := store.ImportSnapshot(ctx, candidate, snapshot); err != nil {
		m.mu.Unlock()
		_ = candidate.Close()
		return err
	}
	if err := store.SaveConfig(m.configPath, cfg); err != nil {
		m.mu.Unlock()
		_ = candidate.Close()
		return err
	}
	m.store = candidate
	m.config = cfg
	m.mu.Unlock()
	return nil
}

func (m *StoreManager) current() (store.Store, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.store == nil {
		return nil, store.ErrInvalidStore
	}
	return m.store, nil
}

func (m *StoreManager) NeedsSetup(ctx context.Context) (bool, error) {
	current, err := m.current()
	if err != nil {
		return true, nil
	}
	return current.NeedsSetup(ctx)
}

func (m *StoreManager) CreateUser(ctx context.Context, input store.CreateUserInput) (store.User, error) {
	current, err := m.current()
	if err != nil {
		return store.User{}, err
	}
	return current.CreateUser(ctx, input)
}

func (m *StoreManager) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	current, err := m.current()
	if err != nil {
		return store.User{}, err
	}
	return current.GetUserByEmail(ctx, email)
}

func (m *StoreManager) GetUserByID(ctx context.Context, id string) (store.User, error) {
	current, err := m.current()
	if err != nil {
		return store.User{}, err
	}
	return current.GetUserByID(ctx, id)
}

func (m *StoreManager) ListUsers(ctx context.Context) ([]store.User, error) {
	current, err := m.current()
	if err != nil {
		return nil, err
	}
	return current.ListUsers(ctx)
}

func (m *StoreManager) UpdateUser(ctx context.Context, id string, input store.UpdateUserInput) (store.User, error) {
	current, err := m.current()
	if err != nil {
		return store.User{}, err
	}
	return current.UpdateUser(ctx, id, input)
}

func (m *StoreManager) DeleteUser(ctx context.Context, id string) error {
	current, err := m.current()
	if err != nil {
		return err
	}
	return current.DeleteUser(ctx, id)
}

func (m *StoreManager) CountAdmins(ctx context.Context) (int, error) {
	current, err := m.current()
	if err != nil {
		return 0, err
	}
	return current.CountAdmins(ctx)
}

func (m *StoreManager) CreateSession(ctx context.Context, session store.Session) error {
	current, err := m.current()
	if err != nil {
		return err
	}
	return current.CreateSession(ctx, session)
}

func (m *StoreManager) GetSession(ctx context.Context, tokenHash string) (store.Session, error) {
	current, err := m.current()
	if err != nil {
		return store.Session{}, err
	}
	return current.GetSession(ctx, tokenHash)
}

func (m *StoreManager) DeleteSession(ctx context.Context, tokenHash string) error {
	current, err := m.current()
	if err != nil {
		return err
	}
	return current.DeleteSession(ctx, tokenHash)
}

func (m *StoreManager) GetPromptSettings(ctx context.Context) (store.PromptSettings, error) {
	current, err := m.current()
	if err != nil {
		return store.PromptSettings{}, err
	}
	return current.GetPromptSettings(ctx)
}

func (m *StoreManager) SavePromptSettings(ctx context.Context, settings store.PromptSettings) error {
	current, err := m.current()
	if err != nil {
		return err
	}
	return current.SavePromptSettings(ctx, settings)
}
