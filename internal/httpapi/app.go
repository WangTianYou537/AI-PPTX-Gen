package httpapi

import (
	"time"

	"wty5.cn/ppt-gen/internal/store"
)

type Server struct {
	store         store.Store
	sessionCookie string
	sessionTTL    time.Duration
}

func New(store store.Store) *Server {
	return &Server{
		store:         store,
		sessionCookie: "ppt_session",
		sessionTTL:    7 * 24 * time.Hour,
	}
}
