package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/store"
)

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type setupStatusResponse struct {
	NeedsSetup bool `json:"needsSetup"`
}

type meResponse struct {
	User store.User `json:"user"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	needsSetup, err := s.store.NeedsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{NeedsSetup: needsSetup})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	needsSetup, err := s.store.NeedsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !needsSetup {
		writeError(w, http.StatusForbidden, "系统已经完成安装")
		return
	}
	var input authRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	user, token, err := s.createUserAndSession(r, input.Email, input.Password, store.RoleAdmin)
	if err != nil {
		handleAuthError(w, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, meResponse{User: user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input authRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	user, err := s.store.GetUserByEmail(r.Context(), input.Email)
	if err != nil || user.Disabled || !auth.CheckPassword(input.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "邮箱或密码不正确")
		return
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	if err := s.store.CreateSession(r.Context(), store.Session{ID: newSessionID(), UserID: user.ID, TokenHash: tokenHash, ExpiresAt: now.Add(s.sessionTTL), CreatedAt: now}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, meResponse{User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(s.sessionCookie); err == nil && cookie.Value != "" {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(cookie.Value))
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.authenticate(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: user})
}

func (s *Server) createUserAndSession(r *http.Request, email, password, role string) (store.User, string, error) {
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") {
		return store.User{}, "", errBadRequest("请输入有效邮箱")
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return store.User{}, "", errBadRequest(err.Error())
	}
	user, err := s.store.CreateUser(r.Context(), store.CreateUserInput{Email: email, PasswordHash: passwordHash, Role: role})
	if err != nil {
		return store.User{}, "", err
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		return store.User{}, "", err
	}
	now := time.Now().UTC()
	err = s.store.CreateSession(r.Context(), store.Session{ID: newSessionID(), UserID: user.ID, TokenHash: tokenHash, ExpiresAt: now.Add(s.sessionTTL), CreatedAt: now})
	return user, token, err
}

type badRequestError string

func (e badRequestError) Error() string { return string(e) }

func errBadRequest(message string) error { return badRequestError(message) }

func handleAuthError(w http.ResponseWriter, err error) {
	var bad badRequestError
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusBadRequest, "邮箱已存在")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func newSessionID() string {
	token, _, err := auth.NewToken()
	if err != nil {
		panic(err)
	}
	return token
}
