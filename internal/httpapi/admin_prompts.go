package httpapi

import (
	"encoding/json"
	"net/http"

	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/store"
)

type promptSettingsRequest struct {
	Architect store.GenerationRoleSettings `json:"architect"`
	SVG       store.GenerationRoleSettings `json:"svg"`
	Theme     store.GenerationRoleSettings `json:"theme"`
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
		if err := validateRoleSettings("主题色策划师", input.Theme); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, _ := s.currentUser(r)
		// Preserve workflow JSON when saving roles.
		prev, _ := s.promptSettings(r)
		settings := store.PromptSettings{
			Architect:             input.Architect,
			SVG:                   input.SVG,
			Theme:                 input.Theme,
			ArchitectWorkflowJSON: prev.ArchitectWorkflowJSON,
			UpdatedBy:             user.ID,
		}
		if err := s.dataStore().SavePromptSettings(r.Context(), settings); err != nil {
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
	settings.Theme.SystemPrompt = ppt.DefaultThemeSystemPrompt
	settings.UpdatedBy = user.ID
	if err := s.dataStore().SavePromptSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, _ = s.promptSettings(r)
	writeJSON(w, http.StatusOK, settings)
}
