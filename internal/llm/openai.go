package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

type openAIRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Stream     bool            `json:"stream"`
	Tools      []openAITool    `json:"tools,omitempty"`
	ToolChoice any             `json:"tool_choice,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type openAIChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content   any `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
	// Some OpenAI-compatible gateways return top-level content.
	Content any    `json:"content"`
	Output  string `json:"output"`
	Text    string `json:"text"`
}

func openAITools(tools []ToolDefinition) []openAITool {
	converted := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, openAITool{Type: "function", Function: openAIFunction{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}})
	}
	return converted
}

func generateOpenAI(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	baseURL := normalizeBaseURL(req.Config.BaseURL, defaultOpenAIBaseURL)
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, msg := range req.Messages {
		messages = append(messages, openAIMessage{Role: msg.Role, Content: openAIContentFromMessage(msg)})
	}

	// Default to streaming; incremental JSON can override with "stream": false.
	payload := openAIRequest{
		Model:    req.Config.Model,
		Messages: messages,
		Stream:   true,
	}
	if len(req.Tools) > 0 {
		payload.Tools = openAITools(req.Tools)
		if req.ToolChoice != "" {
			payload.ToolChoice = map[string]any{"type": "function", "function": map[string]string{"name": req.ToolChoice}}
		}
	}

	bodyPayload, err := requestBody(payload, req)
	if err != nil {
		return GenerateResponse{}, err
	}
	headers := map[string]string{"Authorization": "Bearer " + req.Config.APIKey}
	endpoint := baseURL + "/chat/completions"

	if payloadWantsStream(bodyPayload) {
		response, status, body, err := postJSONStream(ctx, endpoint, headers, bodyPayload, req.Config.Proxy)
		if err != nil {
			// Some gateways claim stream support but return plain JSON; try non-stream recovery from body.
			if len(body) > 0 {
				if recovered, recErr := parseOpenAIChatCompletionFlexible(body); recErr == nil && (recovered.Text != "" || recovered.ToolName != "") {
					return recovered, nil
				}
			}
			return GenerateResponse{}, err
		}
		if status < 200 || status >= 300 {
			return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
		}
		if response.Text != "" || response.ToolName != "" {
			return response, nil
		}
		// Empty stream content: try parse raw body as non-stream JSON, then hard fallback to non-stream request.
		if len(body) > 0 {
			if recovered, recErr := parseOpenAIChatCompletionFlexible(body); recErr == nil && (recovered.Text != "" || recovered.ToolName != "") {
				return recovered, nil
			}
		}
		// Automatic non-stream retry for OpenAI-compatible gateways that mishandle SSE.
		nonStreamPayload := forceStreamFlag(bodyPayload, false)
		body2, status2, err2 := postJSON(ctx, endpoint, headers, nonStreamPayload, req.Config.Proxy)
		if err2 != nil {
			return GenerateResponse{}, err2
		}
		if status2 < 200 || status2 >= 300 {
			return GenerateResponse{}, providerError(req.Config.Provider, status2, string(body2))
		}
		response2, err2 := parseOpenAIChatCompletionFlexible(body2)
		if err2 != nil {
			return GenerateResponse{}, err2
		}
		if response2.Text == "" && response2.ToolName == "" {
			return GenerateResponse{}, NewUserError("OpenAI 响应中没有文本内容（流式与非流式均无有效输出）")
		}
		return response2, nil
	}

	body, status, err := postJSON(ctx, endpoint, headers, bodyPayload, req.Config.Proxy)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}
	response, err := parseOpenAIChatCompletionFlexible(body)
	if err != nil {
		return GenerateResponse{}, err
	}
	if response.Text == "" && response.ToolName == "" {
		return GenerateResponse{}, NewUserError("OpenAI 响应中没有文本内容")
	}
	return response, nil
}

func forceStreamFlag(body any, stream bool) any {
	switch v := body.(type) {
	case map[string]any:
		cp := make(map[string]any, len(v)+1)
		for k, val := range v {
			cp[k] = val
		}
		cp["stream"] = stream
		return cp
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return body
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return body
		}
		m["stream"] = stream
		return m
	}
}

func parseOpenAIChatCompletion(body []byte) (GenerateResponse, error) {
	return parseOpenAIChatCompletionFlexible(body)
}

func parseOpenAIChatCompletionFlexible(body []byte) (GenerateResponse, error) {
	// First try standard chat.completion schema.
	var parsed openAIChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err == nil {
		for _, choice := range parsed.Choices {
			for _, call := range choice.Message.ToolCalls {
				if call.Function.Name != "" {
					args := strings.TrimSpace(call.Function.Arguments)
					if args == "" {
						args = "{}"
					}
					return GenerateResponse{
						Text:      anyToString(choice.Message.Content),
						ToolName:  call.Function.Name,
						ToolInput: json.RawMessage(args),
					}, nil
				}
			}
			if text := strings.TrimSpace(anyToString(choice.Message.Content)); text != "" {
				return GenerateResponse{Text: text}, nil
			}
			if text := strings.TrimSpace(choice.Text); text != "" {
				return GenerateResponse{Text: text}, nil
			}
		}
		if text := strings.TrimSpace(anyToString(parsed.Content)); text != "" {
			return GenerateResponse{Text: text}, nil
		}
		if text := strings.TrimSpace(parsed.Output); text != "" {
			return GenerateResponse{Text: text}, nil
		}
		if text := strings.TrimSpace(parsed.Text); text != "" {
			return GenerateResponse{Text: text}, nil
		}
	}

	// Fallback: generic map walk for OpenAI-compatible proxies.
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		return GenerateResponse{}, err
	}
	if text := firstStringByKeys(generic, "output_text", "text", "content", "response", "message"); text != "" {
		return GenerateResponse{Text: text}, nil
	}
	// choices[0].message.content / choices[0].text / choices[0].delta.content
	if choices, ok := generic["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				if text := strings.TrimSpace(anyToString(msg["content"])); text != "" {
					return GenerateResponse{Text: text}, nil
				}
			}
			if delta, ok := c0["delta"].(map[string]any); ok {
				if text := strings.TrimSpace(anyToString(delta["content"])); text != "" {
					return GenerateResponse{Text: text}, nil
				}
			}
			if text := strings.TrimSpace(anyToString(c0["text"])); text != "" {
				return GenerateResponse{Text: text}, nil
			}
		}
	}
	return GenerateResponse{}, nil
}

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, item := range t {
			switch part := item.(type) {
			case string:
				b.WriteString(part)
			case map[string]any:
				if text, ok := part["text"].(string); ok {
					b.WriteString(text)
				} else if text, ok := part["content"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	case map[string]any:
		if text, ok := t["text"].(string); ok {
			return text
		}
		if text, ok := t["content"].(string); ok {
			return text
		}
		raw, _ := json.Marshal(t)
		return string(raw)
	default:
		raw, _ := json.Marshal(t)
		return string(raw)
	}
}

func firstStringByKeys(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := strings.TrimSpace(anyToString(m[key])); text != "" {
			return text
		}
	}
	return ""
}

func openAIContentFromMessage(msg Message) any {
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
			dataURL := "data:" + mime + ";base64," + encodeBase64(p.Data)
			if strings.HasPrefix(mime, "image/") {
				parts = append(parts, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": dataURL},
				})
			} else if strings.Contains(mime, "pdf") {
				filename := strings.TrimSpace(p.Filename)
				if filename == "" {
					filename = "document.pdf"
				}
				parts = append(parts, map[string]any{
					"type": "file",
					"file": map[string]any{
						"filename":  filename,
						"file_data": dataURL,
					},
				})
			} else {
				filename := strings.TrimSpace(p.Filename)
				if filename == "" {
					filename = "attachment.bin"
				}
				parts = append(parts, map[string]any{
					"type": "file",
					"file": map[string]any{
						"filename":  filename,
						"file_data": dataURL,
					},
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

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
