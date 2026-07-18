package llm

import (
	"context"
	"encoding/json"
	"strings"
)

// OpenAI Responses API provider kind.
// Docs: POST /v1/responses with input/instructions/tools.
const ProviderOpenAIResponses Provider = "openai-responses"

type openAIResponsesRequest struct {
	Model        string                   `json:"model"`
	Instructions string                   `json:"instructions,omitempty"`
	Input        []openAIResponsesMessage `json:"input"`
	Tools        []openAIResponsesTool    `json:"tools,omitempty"`
	ToolChoice   any                      `json:"tool_choice,omitempty"`
	Text         *openAIResponsesText     `json:"text,omitempty"`
	Stream       bool                     `json:"stream"`
}

type openAIResponsesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIResponsesText struct {
	Format map[string]string `json:"format,omitempty"`
}

type openAIResponsesAPIResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Content   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func openAIResponsesTools(tools []ToolDefinition) []openAIResponsesTool {
	converted := make([]openAIResponsesTool, 0, len(tools))
	for _, tool := range tools {
		// Responses API uses flattened function tool schema.
		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		converted = append(converted, openAIResponsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}
	return converted
}

func generateOpenAIResponses(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	baseURL := normalizeBaseURL(req.Config.BaseURL, defaultOpenAIBaseURL)
	input := make([]openAIResponsesMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := msg.Role
		if role != "assistant" && role != "system" {
			role = "user"
		}
		// Responses API typically uses user/assistant in input; system goes to instructions.
		if role == "system" {
			continue
		}
		input = append(input, openAIResponsesMessage{Role: role, Content: msg.Content})
	}
	// If no user messages were provided, fall back to a single empty user message so the API accepts the request.
	if len(input) == 0 {
		input = append(input, openAIResponsesMessage{Role: "user", Content: " "})
	}

	payload := openAIResponsesRequest{
		Model:        req.Config.Model,
		Instructions: req.SystemPrompt,
		Input:        input,
		Stream:       false,
	}
	if req.JSONMode {
		payload.Text = &openAIResponsesText{Format: map[string]string{"type": "json_object"}}
	}
	if len(req.Tools) > 0 {
		payload.Tools = openAIResponsesTools(req.Tools)
		if req.ToolChoice != "" {
			// Force a specific function tool when requested.
			payload.ToolChoice = map[string]any{"type": "function", "name": req.ToolChoice}
		}
	}

	bodyPayload, err := mergePayloadJSON(payload, req.Extra)
	if err != nil {
		return GenerateResponse{}, err
	}

	body, status, err := postJSON(ctx, baseURL+"/responses", map[string]string{
		"Authorization": "Bearer " + req.Config.APIKey,
	}, bodyPayload)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}

	var parsed openAIResponsesAPIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return GenerateResponse{}, err
	}

	// Prefer function_call outputs when tools are used.
	for _, item := range parsed.Output {
		if item.Type == "function_call" && item.Name != "" {
			args := strings.TrimSpace(item.Arguments)
			if args == "" {
				args = "{}"
			}
			return GenerateResponse{ToolName: item.Name, ToolInput: json.RawMessage(args)}, nil
		}
	}

	if text := strings.TrimSpace(parsed.OutputText); text != "" {
		return GenerateResponse{Text: text}, nil
	}
	var text strings.Builder
	for _, item := range parsed.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" || part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
	}
	if text.Len() == 0 {
		return GenerateResponse{}, NewUserError("OpenAI Responses 响应中没有文本内容")
	}
	return GenerateResponse{Text: text.String()}, nil
}
