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
