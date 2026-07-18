package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/store"
)

type adminSettingsRequest struct {
	RegistrationEnabled          bool   `json:"registrationEnabled"`
	DefaultUserGroupID           string `json:"defaultUserGroupId"`
	DefaultSlideConcurrencyLimit int    `json:"defaultSlideConcurrencyLimit"`
}

type userGroupRequest struct {
	Name                  *string `json:"name"`
	Description           *string `json:"description"`
	DailyPPTLimit         *int    `json:"dailyPPTLimit"`
	DailySlideLimit       *int    `json:"dailySlideLimit"`
	SlideConcurrencyLimit *int    `json:"slideConcurrencyLimit"`
	IsDefault             *bool   `json:"isDefault"`
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.dataStore().GetSystemSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var input adminSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		user, _ := s.currentUser(r)
		settings := store.SystemSettings{RegistrationEnabled: input.RegistrationEnabled, DefaultUserGroupID: input.DefaultUserGroupID, DefaultSlideConcurrencyLimit: input.DefaultSlideConcurrencyLimit, UpdatedBy: user.ID}
		if err := validateConcurrencyLimit(&settings.DefaultSlideConcurrencyLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.dataStore().SaveSystemSettings(r.Context(), settings); err != nil {
			handleAdminStoreError(w, err)
			return
		}
		settings, _ = s.dataStore().GetSystemSettings(r.Context())
		writeJSON(w, http.StatusOK, settings)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := s.dataStore().ListUserGroups(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
	case http.MethodPost:
		var input userGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		if input.Name == nil || strings.TrimSpace(*input.Name) == "" {
			writeError(w, http.StatusBadRequest, "请填写用户组名称")
			return
		}
		pptLimit, slideLimit := 0, 100
		if input.DailyPPTLimit != nil {
			pptLimit = *input.DailyPPTLimit
		}
		if input.DailySlideLimit != nil {
			slideLimit = *input.DailySlideLimit
		}
		if err := validateLimits(&pptLimit, &slideLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateConcurrencyLimit(input.SlideConcurrencyLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		description := ""
		if input.Description != nil {
			description = *input.Description
		}
		isDefault := false
		if input.IsDefault != nil {
			isDefault = *input.IsDefault
		}
		group, err := s.dataStore().CreateUserGroup(r.Context(), store.CreateUserGroupInput{Name: *input.Name, Description: description, DailyPPTLimit: pptLimit, DailySlideLimit: slideLimit, SlideConcurrencyLimit: concurrencyLimitValue(input.SlideConcurrencyLimit), IsDefault: isDefault})
		if err != nil {
			handleAdminStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, group)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminGroup(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/groups/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "用户组不存在")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var input userGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		update := store.UpdateUserGroupInput{Name: input.Name, Description: input.Description, DailyPPTLimit: input.DailyPPTLimit, DailySlideLimit: input.DailySlideLimit, SlideConcurrencyLimit: input.SlideConcurrencyLimit, IsDefault: input.IsDefault}
		if err := validateLimits(input.DailyPPTLimit, input.DailySlideLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateConcurrencyLimit(input.SlideConcurrencyLimit); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		group, err := s.dataStore().UpdateUserGroup(r.Context(), id, update)
		if err != nil {
			handleAdminStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, group)
	case http.MethodDelete:
		if err := s.dataStore().DeleteUserGroup(r.Context(), id); err != nil {
			handleAdminStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func concurrencyLimitValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
