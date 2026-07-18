package httpapi

import (
	"context"
	"sync"

	"wty5.cn/ppt-gen/internal/store"
)

// StoreManager owns the currently active store and storage config lifecycle.
type StoreManager struct {
	mu         sync.RWMutex
	store      store.Store
	config     store.Config
	configPath string
}

func NewStoreManager(active store.Store, cfg store.Config, configPath string) *StoreManager {
	return &StoreManager{store: active, config: cfg, configPath: configPath}
}

func (m *StoreManager) Store() (store.Store, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.store == nil {
		return nil, store.ErrInvalidStore
	}
	return m.store, nil
}

func (m *StoreManager) Current() (store.Store, bool) {
	s, err := m.Store()
	return s, err == nil
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
