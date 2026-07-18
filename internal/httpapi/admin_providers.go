package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/store"
)

type llmProviderRequest struct {
	Name    *string `json:"name"`
	Kind    *string `json:"kind"`
	BaseURL *string `json:"baseURL"`
	APIKey  *string `json:"apiKey"`
	Enabled *bool   `json:"enabled"`
}

func (s *Server) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		providers, err := s.dataStore().ListLLMProviders(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// redact api keys in list
		out := make([]store.LLMProvider, 0, len(providers))
		for _, p := range providers {
			p.APIKey = redactKey(p.APIKey)
			out = append(out, p)
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	case http.MethodPost:
		var input llmProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		if input.Name == nil || strings.TrimSpace(*input.Name) == "" {
			writeError(w, http.StatusBadRequest, "请填写提供商名称")
			return
		}
		if input.Kind == nil || !validProviderKind(*input.Kind) {
			writeError(w, http.StatusBadRequest, "提供商类型不正确")
			return
		}
		if input.APIKey == nil || strings.TrimSpace(*input.APIKey) == "" {
			writeError(w, http.StatusBadRequest, "请填写 API Key")
			return
		}
		baseURL := ""
		if input.BaseURL != nil {
			baseURL = *input.BaseURL
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		provider, err := s.dataStore().CreateLLMProvider(r.Context(), store.CreateLLMProviderInput{
			Name: *input.Name, Kind: normalizeProviderKind(*input.Kind), BaseURL: baseURL, APIKey: *input.APIKey, Enabled: enabled,
		})
		if err != nil {
			handleAdminStoreError(w, err)
			return
		}
		provider.APIKey = redactKey(provider.APIKey)
		writeJSON(w, http.StatusOK, provider)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/providers/")
	if id == "" || strings.Contains(id, "/") {
		// models subpath handled separately
		if strings.HasSuffix(r.URL.Path, "/models") {
			s.handleAdminProviderModels(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "提供商不存在")
		return
	}
	// /api/admin/providers/{id}/models
	if strings.HasSuffix(id, "/models") {
		s.handleAdminProviderModels(w, r)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var input llmProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
			return
		}
		if input.Kind != nil && !validProviderKind(*input.Kind) {
			writeError(w, http.StatusBadRequest, "提供商类型不正确")
			return
		}
		provider, err := s.dataStore().UpdateLLMProvider(r.Context(), id, store.UpdateLLMProviderInput{
			Name: input.Name, Kind: input.Kind, BaseURL: input.BaseURL, APIKey: input.APIKey, Enabled: input.Enabled,
		})
		if err != nil {
			handleAdminStoreError(w, err)
			return
		}
		provider.APIKey = redactKey(provider.APIKey)
		writeJSON(w, http.StatusOK, provider)
	case http.MethodDelete:
		if err := s.dataStore().DeleteLLMProvider(r.Context(), id); err != nil {
			handleAdminStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodGet:
		provider, err := s.dataStore().GetLLMProvider(r.Context(), id)
		if err != nil {
			handleAdminStoreError(w, err)
			return
		}
		provider.APIKey = redactKey(provider.APIKey)
		writeJSON(w, http.StatusOK, provider)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/providers/")
	id := strings.TrimSuffix(path, "/models")
	id = strings.TrimSuffix(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "提供商不存在")
		return
	}
	provider, err := s.dataStore().GetLLMProvider(r.Context(), id)
	if err != nil {
		handleAdminStoreError(w, err)
		return
	}
	// Optional temporary override via POST body for unsaved form values.
	cfg := llm.Config{Provider: llm.Provider(provider.Kind), APIKey: provider.APIKey, BaseURL: provider.BaseURL, Model: "dummy"}
	if r.Method == http.MethodPost {
		var input llmProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err == nil {
			if input.Kind != nil && validProviderKind(*input.Kind) {
				cfg.Provider = llm.Provider(normalizeProviderKind(*input.Kind))
			}
			if input.BaseURL != nil {
				cfg.BaseURL = *input.BaseURL
			}
			if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" && !strings.Contains(*input.APIKey, "*") {
				cfg.APIKey = *input.APIKey
			}
		}
	}
	// Validate needs model; use placeholder model id.
	if cfg.Model == "" {
		cfg.Model = "model"
	}
	models, err := llm.ListModels(r.Context(), cfg)
	if err != nil {
		var userErr llm.UserError
		if errors.As(err, &userErr) {
			writeError(w, http.StatusBadRequest, userErr.Error())
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func validProviderKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(llm.ProviderOpenAI), string(llm.ProviderOpenAIResponses), "responses", string(llm.ProviderGemini), string(llm.ProviderClaude):
		return true
	default:
		return false
	}
}

func normalizeProviderKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "responses" {
		return string(llm.ProviderOpenAIResponses)
	}
	return kind
}

func redactKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}
