package llm

import (
	"encoding/json"
	"strings"
)

type Provider string

const (
	ProviderOpenAI          Provider = "openai"
	ProviderOpenAIResponses Provider = "openai-responses"
	ProviderGemini          Provider = "gemini"
	ProviderClaude          Provider = "claude"
)

type Config struct {
	Provider Provider `json:"provider"`
	APIKey   string   `json:"apiKey"`
	BaseURL  string   `json:"baseURL"`
	Model    string   `json:"model"`
	// Proxy is optional HTTP/HTTPS/SOCKS proxy URL for this request.
	Proxy string `json:"proxy,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Parts enables multimodal user content (text + inline PDF/image bytes).
	// When non-empty, providers should prefer Parts over Content.
	Parts []ContentPart `json:"parts,omitempty"`
}

// ContentPart is a multimodal content fragment.
// Type: "text" | "file"
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	// Data is raw file bytes (not base64). Providers encode as needed.
	Data []byte `json:"-"`
	// Filename is optional display name for logging/debug.
	Filename string `json:"filename,omitempty"`
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
	// Payload is an optional complete provider request body.
	// When non-nil, providers send this object as-is (instead of building the default body).
	Payload map[string]any
	// Extra is merged into the provider-specific request body after the base payload is built.
	// Used only when Payload is nil.
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

func requestBody(defaultPayload any, req GenerateRequest) (any, error) {
	if req.Payload != nil {
		return req.Payload, nil
	}
	return mergePayloadJSON(defaultPayload, req.Extra)
}

// payloadWantsStream inspects the final request body and decides whether to use SSE transport.
// Missing stream defaults to false for safety unless provider code set it true before merge.
func payloadWantsStream(body any) bool {
	switch v := body.(type) {
	case map[string]any:
		raw, ok := v["stream"]
		if !ok {
			return false
		}
		switch b := raw.(type) {
		case bool:
			return b
		case string:
			return strings.EqualFold(strings.TrimSpace(b), "true")
		default:
			return false
		}
	default:
		// Struct payloads are marshaled again by post helpers; detect via JSON.
		raw, err := json.Marshal(body)
		if err != nil {
			return false
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return false
		}
		return payloadWantsStream(m)
	}
}
