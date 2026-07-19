package llm

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OpenAI Responses API docs: POST /v1/responses with input/instructions/tools.

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
	Content any    `json:"content"`
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
		input = append(input, openAIResponsesMessage{Role: role, Content: openAIResponsesContentFromMessage(msg)})
	}
	// If no user messages were provided, fall back to a single empty user message so the API accepts the request.
	if len(input) == 0 {
		input = append(input, openAIResponsesMessage{Role: "user", Content: " "})
	}

	// Default non-stream; incremental JSON can enable "stream": true.
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

	bodyPayload, err := requestBody(payload, req)
	if err != nil {
		return GenerateResponse{}, err
	}
	headers := map[string]string{"Authorization": "Bearer " + req.Config.APIKey}
	endpoint := baseURL + "/responses"

	if payloadWantsStream(bodyPayload) {
		response, status, body, err := postJSONStream(ctx, endpoint, headers, bodyPayload, req.Config.Proxy)
		if status < 200 || status >= 300 {
			if err != nil && len(body) == 0 {
				return GenerateResponse{}, err
			}
			if len(body) > 0 {
				return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
			}
			if err != nil {
				return GenerateResponse{}, err
			}
			return GenerateResponse{}, providerError(req.Config.Provider, status, "")
		}
		// Chat-completions SSE parser may not understand Responses events; fall back to Responses stream parser.
		if (err != nil || (response.Text == "" && response.ToolName == "")) && len(body) > 0 {
			parsed, parseErr := parseOpenAIResponsesStream(body)
			if parseErr == nil && (parsed.Text != "" || parsed.ToolName != "") {
				return parsed, nil
			}
			if err != nil {
				return GenerateResponse{}, err
			}
			if parseErr != nil {
				return GenerateResponse{}, parseErr
			}
			response = parsed
		} else if err != nil {
			return GenerateResponse{}, err
		}
		if response.Text == "" && response.ToolName == "" {
			return GenerateResponse{}, NewUserError("OpenAI Responses 流式响应中没有文本内容")
		}
		return response, nil
	}

	body, status, err := postJSON(ctx, endpoint, headers, bodyPayload, req.Config.Proxy)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}
	response, err := parseOpenAIResponses(body)
	if err != nil {
		return GenerateResponse{}, err
	}
	if response.Text == "" && response.ToolName == "" {
		return GenerateResponse{}, NewUserError("OpenAI Responses 响应中没有文本内容")
	}
	return response, nil
}

func parseOpenAIResponses(body []byte) (GenerateResponse, error) {
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
	return GenerateResponse{Text: text.String()}, nil
}

// parseOpenAIResponsesStream handles Responses API SSE events from a raw stream body dump.
// Supported event payloads include response.output_text.delta and response.function_call_arguments.delta.
func parseOpenAIResponsesStream(raw []byte) (GenerateResponse, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var text strings.Builder
	var toolName string
	var toolArgs strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		etype, _ := event["type"].(string)
		switch etype {
		case "response.output_text.delta":
			if delta, ok := event["delta"].(string); ok {
				text.WriteString(delta)
			}
		case "response.function_call_arguments.delta":
			if delta, ok := event["delta"].(string); ok {
				toolArgs.WriteString(delta)
			}
			if name, ok := event["name"].(string); ok && name != "" {
				toolName = name
			}
		case "response.output_item.added", "response.output_item.done":
			if item, ok := event["item"].(map[string]any); ok {
				if typ, _ := item["type"].(string); typ == "function_call" {
					if name, _ := item["name"].(string); name != "" {
						toolName = name
					}
					if args, _ := item["arguments"].(string); args != "" {
						toolArgs.Reset()
						toolArgs.WriteString(args)
					}
				}
			}
		case "response.completed":
			// no-op; fall through to return accumulated values
		}
	}
	if err := scanner.Err(); err != nil {
		return GenerateResponse{}, fmt.Errorf("解析 OpenAI Responses 流式响应失败: %w", err)
	}
	if toolName != "" {
		args := strings.TrimSpace(toolArgs.String())
		if args == "" {
			args = "{}"
		}
		return GenerateResponse{Text: text.String(), ToolName: toolName, ToolInput: json.RawMessage(args)}, nil
	}
	return GenerateResponse{Text: text.String()}, nil
}

// silence unused import if any older code paths referenced io
var _ = io.EOF

func openAIResponsesContentFromMessage(msg Message) any {
	if len(msg.Parts) == 0 {
		return msg.Content
	}
	// Responses API content can be an array of typed parts.
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
			dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(p.Data)
			if strings.HasPrefix(mime, "image/") {
				parts = append(parts, map[string]any{
					"type":      "input_image",
					"image_url": dataURL,
				})
			} else {
				parts = append(parts, map[string]any{
					"type":      "input_file",
					"filename":  p.Filename,
					"file_data": dataURL,
				})
			}
		default:
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			parts = append(parts, map[string]any{"type": "input_text", "text": p.Text})
		}
	}
	if len(parts) == 0 {
		return msg.Content
	}
	return parts
}
