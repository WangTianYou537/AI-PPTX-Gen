package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/store"
)

type contextKey string

const userContextKey contextKey = "user"

func (s *Server) currentUser(r *http.Request) (store.User, bool) {
	user, ok := r.Context().Value(userContextKey).(store.User)
	return user, ok
}

func (s *Server) authenticate(r *http.Request) (store.User, bool) {
	requestID := requestIDFromContext(r.Context())
	cookie, err := r.Cookie(s.sessionCookie)
	if err != nil || cookie.Value == "" {
		if debugEnabled.Load() {
			log.Printf("auth failed request_id=%s reason=missing_cookie", requestID)
		}
		return store.User{}, false
	}
	session, err := s.store.GetSession(r.Context(), auth.HashToken(cookie.Value))
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("auth failed request_id=%s reason=session_lookup err=%v", requestID, err)
		}
		return store.User{}, false
	}
	if session.ExpiresAt.Before(time.Now().UTC()) {
		if debugEnabled.Load() {
			log.Printf("auth failed request_id=%s reason=session_expired user_id=%s", requestID, session.UserID)
		}
		return store.User{}, false
	}
	user, err := s.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("auth failed request_id=%s reason=user_lookup user_id=%s err=%v", requestID, session.UserID, err)
		}
		return store.User{}, false
	}
	if user.Disabled {
		if debugEnabled.Load() {
			log.Printf("auth failed request_id=%s reason=user_disabled user_id=%s", requestID, user.ID)
		}
		return store.User{}, false
	}
	return user, true
}

func (s *Server) withUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.withUser(func(w http.ResponseWriter, r *http.Request) {
		user, _ := s.currentUser(r)
		if user.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(s.sessionTTL),
		MaxAge:   int(s.sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
