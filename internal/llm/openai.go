package llm

import "context"

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
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
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
		messages = append(messages, openAIMessage{Role: msg.Role, Content: msg.Content})
	}

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

	response, status, body, err := postJSONStream(ctx, baseURL+"/chat/completions", map[string]string{
		"Authorization": "Bearer " + req.Config.APIKey,
	}, payload)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}
	if response.Text == "" && response.ToolName == "" {
		return GenerateResponse{}, NewUserError("OpenAI 流式响应中没有文本内容")
	}
	return response, nil
}
