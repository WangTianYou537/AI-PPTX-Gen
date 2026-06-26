package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/store"
)

type authRequest struct {
	Email    string       `json:"email"`
	Password string       `json:"password"`
	Storage  store.Config `json:"storage"`
}

type setupStatusResponse struct {
	NeedsSetup        bool                  `json:"needsSetup"`
	StorageConfigured bool                  `json:"storageConfigured"`
	Storage           *store.Config         `json:"storage,omitempty"`
	SupportedStorage  []store.StorageOption `json:"supportedStorage"`
}

type meResponse struct {
	User store.User `json:"user"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	currentConfig, storageConfigured := s.stores.Config()
	needsSetup := true
	if storageConfigured {
		var err error
		needsSetup, err = s.store.NeedsSetup(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	var storagePtr *store.Config
	if storageConfigured {
		storagePtr = &currentConfig
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{
		NeedsSetup:        needsSetup,
		StorageConfigured: storageConfigured,
		Storage:           storagePtr,
		SupportedStorage:  store.SupportedStorageOptions(),
	})
}

func (s *Server) handleSetupStorageTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Storage store.Config `json:"storage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if err := s.stores.TestConfig(r.Context(), input.Storage); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input authRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if _, ok := s.stores.Current(); !ok {
		if input.Storage.Kind == "" {
			writeError(w, http.StatusBadRequest, "请选择数据存储方式")
			return
		}
		if err := s.stores.ConfigureInitial(r.Context(), input.Storage); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	needsSetup, err := s.store.NeedsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if needsSetup {
		writeError(w, http.StatusForbidden, "请先完成管理员初始化")
		return
	}
	var input authRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	user, token, err := s.createUserAndSession(r, input.Email, input.Password, store.RoleUser)
	if err != nil {
		handleAuthError(w, err)
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
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(cookie.Value)); err != nil && debugEnabled.Load() {
			log.Printf("logout delete session failed request_id=%s err=%v", requestIDFromContext(r.Context()), err)
		}
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
