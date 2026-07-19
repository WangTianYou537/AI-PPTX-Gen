package httpapi

import (
	"encoding/json"
	"net/http"

	"wty5.cn/ppt-gen/internal/agent"
)

func (s *Server) handleAdminAgentWorkflow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.promptSettings(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		wf, err := agent.ParseWorkflowJSON(settings.ArchitectWorkflowJSON)
		if err != nil {
			wf = agent.DefaultWorkflow()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workflow": wf,
			"defaults": agent.DefaultWorkflow(),
		})
	case http.MethodPut:
		var input struct {
			Workflow agent.Workflow `json:"workflow"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		wf := agent.NormalizeWorkflow(input.Workflow)
		settings, err := s.promptSettings(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		user, _ := s.currentUser(r)
		settings.ArchitectWorkflowJSON = agent.MustWorkflowJSON(wf)
		settings.UpdatedBy = user.ID
		if err := s.dataStore().SavePromptSettings(r.Context(), settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workflow": wf})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminAgentWorkflowReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	settings, err := s.promptSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, _ := s.currentUser(r)
	wf := agent.DefaultWorkflow()
	settings.ArchitectWorkflowJSON = agent.MustWorkflowJSON(wf)
	settings.UpdatedBy = user.ID
	if err := s.dataStore().SavePromptSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": wf})
}
