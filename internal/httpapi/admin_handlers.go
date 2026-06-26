package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/ppt"
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

type promptSettingsRequest struct {
	Architect store.GenerationRoleSettings `json:"architect"`
	SVG       store.GenerationRoleSettings `json:"svg"`
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.store.ListUsers(r.Context())
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
		user, err := s.store.CreateUser(r.Context(), create)
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
	user, err := s.store.UpdateUser(r.Context(), id, update)
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
	user, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		handleAdminStoreError(w, err)
		return
	}
	if user.Role == store.RoleAdmin {
		admins, err := s.store.CountAdmins(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusBadRequest, "不能删除最后一个管理员")
			return
		}
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		handleAdminStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminPrompts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.promptSettings(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var input promptSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		if err := validateRoleSettings("PPT 架构师", input.Architect); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateRoleSettings("PPT-SVG 生成器", input.SVG); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, _ := s.currentUser(r)
		settings := store.PromptSettings{Architect: input.Architect, SVG: input.SVG, UpdatedBy: user.ID}
		if err := s.store.SavePromptSettings(r.Context(), settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settings, _ = s.promptSettings(r)
		writeJSON(w, http.StatusOK, settings)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminPromptsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	user, _ := s.currentUser(r)
	settings, err := s.promptSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.Architect.SystemPrompt = ppt.DefaultArchitectSystemPrompt
	settings.SVG.SystemPrompt = ppt.DefaultSVGSystemPrompt
	settings.UpdatedBy = user.ID
	if err := s.store.SavePromptSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, _ = s.promptSettings(r)
	writeJSON(w, http.StatusOK, settings)
}

func validateRoleSettings(label string, settings store.GenerationRoleSettings) error {
	if err := validatePrompt(settings.SystemPrompt); err != nil {
		return errors.New(label + "提示词" + err.Error())
	}
	config := settings.ModelConfig
	switch config.Provider {
	case string(llm.ProviderOpenAI), string(llm.ProviderGemini), string(llm.ProviderClaude):
	default:
		return errors.New(label + " provider 不正确")
	}
	if strings.TrimSpace(config.Model) == "" {
		return errors.New(label + "模型名称不能为空")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return errors.New(label + " API Key 不能为空")
	}
	return nil
}

func validatePrompt(prompt string) error {
	length := len(strings.TrimSpace(prompt))
	if length == 0 {
		return errors.New("不能为空")
	}
	if length > 20000 {
		return errors.New("不能超过 20000 字符")
	}
	return nil
}

func handleAdminStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "资源不存在")
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusBadRequest, "邮箱已存在")
	case errors.Is(err, store.ErrInvalidStore):
		writeError(w, http.StatusBadRequest, "操作不符合当前数据约束")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func applyNullableLimit(raw map[string]json.RawMessage, key string, target **int, clear *bool) error {
	value, ok := raw[key]
	if !ok {
		return nil
	}
	if string(value) == "null" {
		*clear = true
		return nil
	}
	var parsed int
	if err := json.Unmarshal(value, &parsed); err != nil {
		return errors.New("额度必须是数字或 null")
	}
	*target = &parsed
	return nil
}

func validateLimits(values ...*int) error {
	for _, value := range values {
		if value != nil && *value < 0 {
			return errors.New("额度不能小于 0")
		}
	}
	return nil
}

func validateConcurrencyLimit(value *int) error {
	if value == nil {
		return nil
	}
	if *value < 1 {
		return errors.New("页面生成并发数不能小于 1")
	}
	if *value > store.MaxSlideConcurrencyLimit {
		return errors.New("页面生成并发数不能超过 10")
	}
	return nil
}
