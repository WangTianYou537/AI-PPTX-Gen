package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func ListModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Provider {
	case ProviderOpenAI, ProviderOpenAIResponses:
		return listOpenAIModels(ctx, cfg)
	case ProviderGemini:
		return listGeminiModels(ctx, cfg)
	case ProviderClaude:
		return listClaudeModels(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported provider %s", cfg.Provider)
	}
}

func listOpenAIModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	base := normalizeBaseURL(cfg.BaseURL, defaultOpenAIBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	body, status, err := doHTTP(req, cfg.Proxy)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, providerError(cfg.Provider, status, string(body))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" {
			continue
		}
		out = append(out, ModelInfo{ID: item.ID, Name: item.ID})
	}
	return out, nil
}

func listGeminiModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	base := normalizeBaseURL(cfg.BaseURL, "https://generativelanguage.googleapis.com/v1beta")
	endpoint := base + "/models?key=" + url.QueryEscape(cfg.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	body, status, err := doHTTP(req, cfg.Proxy)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, providerError(cfg.Provider, status, string(body))
	}
	var parsed struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if id == "" {
			continue
		}
		name := item.DisplayName
		if name == "" {
			name = id
		}
		out = append(out, ModelInfo{ID: id, Name: name})
	}
	return out, nil
}

func listClaudeModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	base := normalizeBaseURL(cfg.BaseURL, defaultClaudeBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	body, status, err := doHTTP(req, cfg.Proxy)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, providerError(cfg.Provider, status, string(body))
	}
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Claude sometimes returns {"models":[...]} depending on proxy.
		var alt struct {
			Models []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"models"`
		}
		if err2 := json.Unmarshal(body, &alt); err2 != nil {
			return nil, err
		}
		out := make([]ModelInfo, 0, len(alt.Models))
		for _, item := range alt.Models {
			if item.ID == "" {
				continue
			}
			name := item.Name
			if name == "" {
				name = item.ID
			}
			out = append(out, ModelInfo{ID: item.ID, Name: name})
		}
		return out, nil
	}
	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" {
			continue
		}
		name := item.DisplayName
		if name == "" {
			name = item.ID
		}
		out = append(out, ModelInfo{ID: item.ID, Name: name})
	}
	return out, nil
}

func doHTTP(req *http.Request, proxy string) ([]byte, int, error) {
	client, err := httpClientFor(proxy)
	if err != nil {
		return nil, 0, err
	}
	// list models can use a shorter timeout than long generation calls
	client = &http.Client{Timeout: 30 * time.Second, Transport: client.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
