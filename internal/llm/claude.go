package llm

import (
	"context"
	"encoding/json"
)

const defaultClaudeBaseURL = "https://api.anthropic.com/v1"

type claudeRequest struct {
	Model      string          `json:"model"`
	MaxTokens  int             `json:"max_tokens"`
	System     string          `json:"system,omitempty"`
	Messages   []claudeMessage `json:"messages"`
	Tools      []claudeTool    `json:"tools,omitempty"`
	ToolChoice any             `json:"tool_choice,omitempty"`
}

type claudeTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
}

func claudeTools(tools []ToolDefinition) []claudeTool {
	converted := make([]claudeTool, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, claudeTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return converted
}

func generateClaude(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	baseURL := normalizeBaseURL(req.Config.BaseURL, defaultClaudeBaseURL)
	messages := make([]claudeMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := msg.Role
		if role != "assistant" {
			role = "user"
		}
		messages = append(messages, claudeMessage{Role: role, Content: msg.Content})
	}

	payload := claudeRequest{
		Model:     req.Config.Model,
		MaxTokens: 16000,
		System:    req.SystemPrompt,
		Messages:  messages,
	}
	if len(req.Tools) > 0 {
		payload.Tools = claudeTools(req.Tools)
		if req.ToolChoice != "" {
			payload.ToolChoice = map[string]string{"type": "tool", "name": req.ToolChoice}
		}
	}
	bodyPayload, err := mergePayloadJSON(payload, req.Extra)
	if err != nil {
		return GenerateResponse{}, err
	}
	body, status, err := postJSON(ctx, baseURL+"/messages", map[string]string{
		"x-api-key":         req.Config.APIKey,
		"anthropic-version": "2023-06-01",
	}, bodyPayload)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}

	var parsed claudeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return GenerateResponse{}, err
	}
	if parsed.StopReason == "refusal" {
		return GenerateResponse{}, NewUserError("Claude 拒绝了该请求，请调整主题或提示词后重试")
	}
	for _, block := range parsed.Content {
		if block.Type == "tool_use" && block.Name != "" {
			return GenerateResponse{ToolName: block.Name, ToolInput: block.Input}, nil
		}
	}
	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			return GenerateResponse{Text: block.Text}, nil
		}
	}
	return GenerateResponse{}, NewUserError("Claude 响应中没有文本内容")
}
