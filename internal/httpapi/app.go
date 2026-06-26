package httpapi

import (
	"time"

	"wty5.cn/ppt-gen/internal/store"
)

type Server struct {
	store         store.Store
	stores        *StoreManager
	sessionCookie string
	sessionTTL    time.Duration
}

func New(appStore store.Store) *Server {
	return NewWithStoreManager(NewStoreManager(appStore, store.Config{Kind: store.StorageJSON, Path: "data/app.json"}, store.DefaultConfigPath()))
}

func NewWithStoreManager(manager *StoreManager) *Server {
	return &Server{
		store:         manager,
		stores:        manager,
		sessionCookie: "ppt_session",
		sessionTTL:    7 * 24 * time.Hour,
	}
}
