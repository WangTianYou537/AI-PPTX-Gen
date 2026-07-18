package llm

import (
	"encoding/json"
	"strings"
)

type Provider string

const (
	ProviderOpenAI Provider = "openai"
	ProviderGemini Provider = "gemini"
	ProviderClaude Provider = "claude"
)

type Config struct {
	Provider Provider `json:"provider"`
	APIKey   string   `json:"apiKey"`
	BaseURL  string   `json:"baseURL"`
	Model    string   `json:"model"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type GenerateRequest struct {
	Config       Config    `json:"config"`
	SystemPrompt string    `json:"systemPrompt"`
	Messages     []Message `json:"messages"`
	JSONMode     bool      `json:"jsonMode"`
	Tools        []ToolDefinition
	ToolChoice   string
	// Extra is merged into the provider-specific request body after the base payload is built.
	// Use this for optional provider fields such as temperature/max_tokens/top_p.
	Extra map[string]any
}

type GenerateResponse struct {
	Text      string          `json:"text"`
	ToolName  string          `json:"toolName,omitempty"`
	ToolInput json.RawMessage `json:"toolInput,omitempty"`
}

func (c Config) Validate() error {
	if c.Provider == "" {
		return NewUserError("请选择模型供应商")
	}
	if c.APIKey == "" {
		return NewUserError("请填写 API Key")
	}
	if c.Model == "" {
		return NewUserError("请填写模型名称")
	}
	return nil
}

func normalizeBaseURL(baseURL, fallback string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = fallback
	}
	return strings.TrimRight(baseURL, "/")
}

// mergePayloadJSON marshals base into a map and overlays extra keys on top.
// Extra wins for conflicting keys so admins can override optional request fields.
func mergePayloadJSON(base any, extra map[string]any) (any, error) {
	if len(extra) == 0 {
		return base, nil
	}
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged, nil
}
