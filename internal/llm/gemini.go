package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
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
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
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
		parts := geminiPartsFromMessage(msg)
		if len(parts) == 0 {
			continue
		}
		contents = append(contents, geminiContent{Role: role, Parts: parts})
	}

	payload := geminiRequest{Contents: contents}
	if req.SystemPrompt != "" {
		payload.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.SystemPrompt}}}
	}
	if req.JSONMode {
		payload.GenerationConfig = map[string]string{"responseMimeType": "application/json"}
	}

	bodyPayload, err := requestBody(payload, req)
	if err != nil {
		return GenerateResponse{}, err
	}

	endpoint := baseURL + "/models/" + url.PathEscape(req.Config.Model) + ":generateContent?key=" + url.QueryEscape(req.Config.APIKey)
	body, status, err := postJSON(ctx, endpoint, nil, bodyPayload, req.Config.Proxy)
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

func geminiPartsFromMessage(msg Message) []geminiPart {
	if len(msg.Parts) > 0 {
		out := make([]geminiPart, 0, len(msg.Parts))
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
				out = append(out, geminiPart{InlineData: &geminiInlineData{
					MIMEType: mime,
					Data:     base64.StdEncoding.EncodeToString(p.Data),
				}})
			default:
				if strings.TrimSpace(p.Text) == "" {
					continue
				}
				out = append(out, geminiPart{Text: p.Text})
			}
		}
		return out
	}
	if strings.TrimSpace(msg.Content) == "" {
		return nil
	}
	return []geminiPart{{Text: msg.Content}}
}
