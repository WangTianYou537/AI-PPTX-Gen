package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
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
	Content any    `json:"content"`
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
		messages = append(messages, claudeMessage{Role: role, Content: claudeContentFromMessage(msg)})
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
	bodyPayload, err := requestBody(payload, req)
	if err != nil {
		return GenerateResponse{}, err
	}
	body, status, err := postJSON(ctx, baseURL+"/messages", map[string]string{
		"x-api-key":         req.Config.APIKey,
		"anthropic-version": "2023-06-01",
	}, bodyPayload, req.Config.Proxy)
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

func claudeContentFromMessage(msg Message) any {
	if len(msg.Parts) == 0 {
		return msg.Content
	}
	parts := make([]map[string]any, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		switch strings.ToLower(strings.TrimSpace(p.Type)) {
		case "file", "inline_data", "inline-data", "media":
			if len(p.Data) == 0 {
				continue
			}
			mime := strings.TrimSpace(p.MIMEType)
			if mime == "" {
				mime = "application/octet-stream"
			}
			if strings.HasPrefix(mime, "image/") {
				parts = append(parts, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mime,
						"data":       base64.StdEncoding.EncodeToString(p.Data),
					},
				})
			} else if strings.Contains(mime, "pdf") {
				// Claude document block for PDFs when supported.
				parts = append(parts, map[string]any{
					"type": "document",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mime,
						"data":       base64.StdEncoding.EncodeToString(p.Data),
					},
				})
			} else {
				// Fallback as text note if unsupported binary.
				parts = append(parts, map[string]any{
					"type": "text",
					"text": "[binary attachment omitted: " + p.Filename + "]",
				})
			}
		default:
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			parts = append(parts, map[string]any{"type": "text", "text": p.Text})
		}
	}
	if len(parts) == 0 {
		return msg.Content
	}
	return parts
}
