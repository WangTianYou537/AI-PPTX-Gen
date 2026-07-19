package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PayloadVars are values injectable into a full request payload template.
type PayloadVars struct {
	Model        string
	SystemPrompt string
	UserPrompt   string
	// Optional structured values for advanced templates.
	Messages   any
	Tools      any
	ToolChoice any
	JSONMode   bool
}

// RenderPayloadTemplate treats raw as a full JSON object template.
// String values may contain placeholders such as:
//
//	{{model}} {{systemPrompt}} {{userPrompt}} {{prompt}} {{requestContent}}
//	{{messages}} {{tools}} {{toolChoice}} {{jsonMode}}
//
// If a string is exactly one placeholder for a structured value (messages/tools/toolChoice),
// it is replaced by the structured value instead of a stringified form.
func RenderPayloadTemplate(raw string, vars PayloadVars) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var template any
	if err := json.Unmarshal([]byte(raw), &template); err != nil {
		return nil, fmt.Errorf("请求 Payload 不是合法 JSON: %w", err)
	}
	obj, ok := template.(map[string]any)
	if !ok || obj == nil {
		return nil, fmt.Errorf("请求 Payload 必须是 JSON 对象")
	}
	rendered, err := renderValue(obj, vars)
	if err != nil {
		return nil, err
	}
	out, ok := rendered.(map[string]any)
	if !ok || out == nil {
		return nil, fmt.Errorf("请求 Payload 渲染结果必须是 JSON 对象")
	}
	return out, nil
}

func renderValue(value any, vars PayloadVars) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			next, err := renderValue(child, vars)
			if err != nil {
				return nil, err
			}
			out[key] = next
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			next, err := renderValue(child, vars)
			if err != nil {
				return nil, err
			}
			out = append(out, next)
		}
		return out, nil
	case string:
		return renderString(v, vars)
	default:
		return v, nil
	}
}

func renderString(text string, vars PayloadVars) (any, error) {
	trimmed := strings.TrimSpace(text)
	switch trimmed {
	case "{{messages}}":
		if vars.Messages == nil {
			return []any{}, nil
		}
		return vars.Messages, nil
	case "{{tools}}":
		if vars.Tools == nil {
			return []any{}, nil
		}
		return vars.Tools, nil
	case "{{toolChoice}}", "{{tool_choice}}":
		if vars.ToolChoice == nil {
			return nil, nil
		}
		return vars.ToolChoice, nil
	case "{{jsonMode}}", "{{json_mode}}":
		return vars.JSONMode, nil
	}

	replacer := strings.NewReplacer(
		"{{model}}", vars.Model,
		"{{systemPrompt}}", vars.SystemPrompt,
		"{{system_prompt}}", vars.SystemPrompt,
		"{{userPrompt}}", vars.UserPrompt,
		"{{user_prompt}}", vars.UserPrompt,
		"{{prompt}}", vars.UserPrompt,
		"{{requestContent}}", vars.UserPrompt,
		"{{request_content}}", vars.UserPrompt,
	)
	return replacer.Replace(text), nil
}

// DefaultOpenAIChatPayload is a full payload template for OpenAI Chat Completions.
func DefaultOpenAIChatPayload() string {
	return `{
  "model": "{{model}}",
  "messages": [
    {"role": "system", "content": "{{systemPrompt}}"},
    {"role": "user", "content": "{{userPrompt}}"}
  ],
  "stream": true
}`
}

// DefaultOpenAIResponsesPayload is a full payload template for OpenAI Responses API.
func DefaultOpenAIResponsesPayload() string {
	return `{
  "model": "{{model}}",
  "instructions": "{{systemPrompt}}",
  "input": [
    {"role": "user", "content": "{{userPrompt}}"}
  ],
  "stream": false
}`
}

// DefaultClaudePayload is a full payload template for Anthropic Messages API.
func DefaultClaudePayload() string {
	return `{
  "model": "{{model}}",
  "max_tokens": 16000,
  "system": "{{systemPrompt}}",
  "messages": [
    {"role": "user", "content": "{{userPrompt}}"}
  ]
}`
}

// DefaultGeminiPayload is a full payload template for Gemini generateContent.
func DefaultGeminiPayload() string {
	return `{
  "system_instruction": {
    "parts": [{"text": "{{systemPrompt}}"}]
  },
  "contents": [
    {
      "role": "user",
      "parts": [{"text": "{{userPrompt}}"}]
    }
  ]
}`
}
