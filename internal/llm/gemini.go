package llm

import (
	"context"
	"encoding/json"
	"net/url"
)

const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  any             `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

func generateGemini(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	baseURL := normalizeBaseURL(req.Config.BaseURL, defaultGeminiBaseURL)
	contents := make([]geminiContent, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: msg.Content}}})
	}

	payload := geminiRequest{Contents: contents}
	if req.SystemPrompt != "" {
		payload.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.SystemPrompt}}}
	}
	if req.JSONMode {
		payload.GenerationConfig = map[string]string{"responseMimeType": "application/json"}
	}

	bodyPayload, err := mergePayloadJSON(payload, req.Extra)
	if err != nil {
		return GenerateResponse{}, err
	}

	endpoint := baseURL + "/models/" + url.PathEscape(req.Config.Model) + ":generateContent?key=" + url.QueryEscape(req.Config.APIKey)
	body, status, err := postJSON(ctx, endpoint, nil, bodyPayload)
	if err != nil {
		return GenerateResponse{}, err
	}
	if status < 200 || status >= 300 {
		return GenerateResponse{}, providerError(req.Config.Provider, status, string(body))
	}

	var parsed geminiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return GenerateResponse{}, err
	}
	for _, candidate := range parsed.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				return GenerateResponse{Text: part.Text}, nil
			}
		}
	}
	return GenerateResponse{}, NewUserError("Gemini 响应中没有文本内容")
}
