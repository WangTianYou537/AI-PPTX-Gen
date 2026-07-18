package httpapi

import (
	"time"

	"wty5.cn/ppt-gen/internal/jobs"
	"wty5.cn/ppt-gen/internal/store"
)

type Server struct {
	stores        *StoreManager
	jobs          *jobs.Manager
	sessionCookie string
	sessionTTL    time.Duration
}

func New(appStore store.Store) *Server {
	return NewWithStoreManager(NewStoreManager(appStore, store.Config{Kind: store.StorageJSON, Path: "data/app.json"}, store.DefaultConfigPath()))
}

func NewWithStoreManager(manager *StoreManager) *Server {
	s := &Server{
		stores:        manager,
		sessionCookie: "ppt_session",
		sessionTTL:    7 * 24 * time.Hour,
	}
	s.ensureJobs()
	return s
}

// dataStore returns the active store. Callers must handle ErrInvalidStore via stores.Current first for setup paths.
func (s *Server) dataStore() store.Store {
	current, err := s.stores.Store()
	if err != nil {
		// Unconfigured store: return a sentinel that fails every call with ErrInvalidStore.
		return unconfiguredStore{}
	}
	return current
}
