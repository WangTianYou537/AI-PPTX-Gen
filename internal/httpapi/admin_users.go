package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/store"
)

type adminUserRequest struct {
	Email                 *string `json:"email"`
	Username              *string `json:"username"`
	Password              *string `json:"password"`
	Role                  *string `json:"role"`
	Disabled              *bool   `json:"disabled"`
	GroupID               *string `json:"groupId"`
	DailyPPTLimit         *int    `json:"dailyPPTLimit"`
	DailySlideLimit       *int    `json:"dailySlideLimit"`
	SlideConcurrencyLimit *int    `json:"slideConcurrencyLimit"`
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.dataStore().ListUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	case http.MethodPost:
		var input adminUserRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		if input.Email == nil || input.Password == nil {
			writeError(w, http.StatusBadRequest, "请填写邮箱和密码")
			return
		}
		role := store.RoleUser
		if input.Role != nil {
			role = *input.Role
		}
		if role != store.RoleAdmin && role != store.RoleUser {
			writeError(w, http.StatusBadRequest, "角色不正确")
			return
		}
		passwordHash, err := auth.HashPassword(*input.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		disabled := false
		if input.Disabled != nil {
			disabled = *input.Disabled
		}
		create := store.CreateUserInput{Email: *input.Email, PasswordHash: passwordHash, Role: role, Disabled: disabled, DailyPPTLimit: input.DailyPPTLimit, DailySlideLimit: input.DailySlideLimit, SlideConcurrencyLimit: input.SlideConcurrencyLimit}
		if input.Username != nil {
			create.Username = *input.Username
		}
		if input.GroupID != nil {
			create.GroupID = *input.GroupID
		}
		if err := validateLimits(input.DailyPPTLimit, input.DailySlideLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateConcurrencyLimit(input.SlideConcurrencyLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := s.dataStore().CreateUser(r.Context(), create)
		if err != nil {
			handleAuthError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, user)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	currentUser, _ := s.currentUser(r)
	switch r.Method {
	case http.MethodPatch:
		s.updateAdminUser(w, r, id, currentUser)
	case http.MethodDelete:
		s.deleteAdminUser(w, r, id, currentUser)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateAdminUser(w http.ResponseWriter, r *http.Request, id string, currentUser store.User) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	var input adminUserRequest
	payload, _ := json.Marshal(raw)
	_ = json.Unmarshal(payload, &input)
	var update store.UpdateUserInput
	if input.Email != nil {
		email := strings.TrimSpace(*input.Email)
		update.Email = &email
	}
	if input.Username != nil {
		update.Username = input.Username
	}
	if input.Password != nil && *input.Password != "" {
		passwordHash, err := auth.HashPassword(*input.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		update.PasswordHash = &passwordHash
	}
	if input.Role != nil {
		if *input.Role != store.RoleAdmin && *input.Role != store.RoleUser {
			writeError(w, http.StatusBadRequest, "角色不正确")
			return
		}
		if id == currentUser.ID && *input.Role != store.RoleAdmin {
			writeError(w, http.StatusBadRequest, "不能取消自己的管理员权限")
			return
		}
		update.Role = input.Role
	}
	if input.Disabled != nil {
		if id == currentUser.ID && *input.Disabled {
			writeError(w, http.StatusBadRequest, "不能禁用自己")
			return
		}
		update.Disabled = input.Disabled
	}
	if input.GroupID != nil {
		update.GroupID = input.GroupID
	}
	if err := applyNullableLimit(raw, "dailyPPTLimit", &update.DailyPPTLimit, &update.ClearDailyPPTLimit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyNullableLimit(raw, "dailySlideLimit", &update.DailySlideLimit, &update.ClearDailySlideLimit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyNullableLimit(raw, "slideConcurrencyLimit", &update.SlideConcurrencyLimit, &update.ClearSlideConcurrencyLimit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateLimits(update.DailyPPTLimit, update.DailySlideLimit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateConcurrencyLimit(update.SlideConcurrencyLimit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.dataStore().UpdateUser(r.Context(), id, update)
	if err != nil {
		handleAdminStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) deleteAdminUser(w http.ResponseWriter, r *http.Request, id string, currentUser store.User) {
	if id == currentUser.ID {
		writeError(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	user, err := s.dataStore().GetUserByID(r.Context(), id)
	if err != nil {
		handleAdminStoreError(w, err)
		return
	}
	if user.Role == store.RoleAdmin {
		admins, err := s.dataStore().CountAdmins(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusBadRequest, "不能删除最后一个管理员")
			return
		}
	}
	if err := s.dataStore().DeleteUser(r.Context(), id); err != nil {
		handleAdminStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
