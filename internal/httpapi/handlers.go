package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/store"
)

type architectRequest struct {
	Topic      string `json:"topic"`
	Audience   string `json:"audience"`
	SlideCount int    `json:"slideCount"`
	Style      string `json:"style"`
}

type svgRequest struct {
	Outline ppt.PresentationOutline `json:"outline"`
}

type errorResponse struct {
	Error     string `json:"error"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleArchitect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var input architectRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if input.Topic == "" {
		writeError(w, http.StatusBadRequest, "请填写 PPT 主题")
		return
	}

	settings, err := s.promptSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp, err := s.generateOutline(r, settings.Architect, input)
	if err != nil {
		handleGenerateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGenerateSVG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var input svgRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if len(input.Outline.Slides) == 0 {
		writeError(w, http.StatusBadRequest, "请先生成并确认 PPT 架构")
		return
	}

	settings, err := s.promptSettings(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := ppt.SVGResponse{Slides: make([]ppt.SlideSVG, 0, len(input.Outline.Slides))}
	for index, slide := range input.Outline.Slides {
		started := time.Now()
		if debugEnabled.Load() {
			log.Printf("svg slide generation start request_id=%s index=%d total=%d slide_id=%s title=%q provider=%s model=%s", requestIDFromContext(r.Context()), index+1, len(input.Outline.Slides), slide.ID, slide.Title, settings.SVG.ModelConfig.Provider, settings.SVG.ModelConfig.Model)
		}
		svg, err := s.generateSlideSVG(r, settings.SVG, input.Outline, slide)
		if err != nil {
			if debugEnabled.Load() {
				log.Printf("svg slide generation failed request_id=%s index=%d total=%d slide_id=%s duration=%s err=%v", requestIDFromContext(r.Context()), index+1, len(input.Outline.Slides), slide.ID, time.Since(started), err)
			}
			writeError(w, http.StatusBadGateway, slide.ID+" 生成失败: "+err.Error())
			return
		}
		if debugEnabled.Load() {
			log.Printf("svg slide generation complete request_id=%s index=%d total=%d slide_id=%s duration=%s svg_bytes=%d", requestIDFromContext(r.Context()), index+1, len(input.Outline.Slides), slide.ID, time.Since(started), len(svg))
		}
		response.Slides = append(response.Slides, ppt.SlideSVG{SlideID: slide.ID, Title: slide.Title, SVG: svg})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) generateOutline(r *http.Request, settings store.GenerationRoleSettings, input architectRequest) (ppt.PresentationOutline, error) {
	prompt := ppt.BuildArchitectPrompt(input.Topic, input.Audience, input.Style, input.SlideCount)
	if !settings.SupportsTools {
		prompt += "\n\n输出强制要求：你的回复第一个字符必须是 {，最后一个字符必须是 }。禁止输出任何 JSON 之外的文字。"
	}
	req := llm.GenerateRequest{
		Config:       roleModelConfig(settings.ModelConfig),
		SystemPrompt: settings.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
		JSONMode:     !settings.SupportsTools,
	}
	if settings.SupportsTools {
		req.Tools = []llm.ToolDefinition{createOutlineToolDefinition()}
		req.ToolChoice = "create_outline"
	}
	resp, err := llm.Generate(r.Context(), req)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	if settings.SupportsTools {
		if resp.ToolName != "create_outline" {
			return ppt.PresentationOutline{}, errors.New("模型没有调用 create_outline 工具")
		}
		return ppt.ParseOutline(string(resp.ToolInput))
	}
	return ppt.ParseOutline(resp.Text)
}

func createOutlineToolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "create_outline",
		Description: "Return the PPT outline as structured data.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":    map[string]string{"type": "string"},
				"theme":    map[string]string{"type": "string"},
				"audience": map[string]string{"type": "string"},
				"style":    map[string]string{"type": "string"},
				"slides": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":         map[string]string{"type": "string"},
							"title":      map[string]string{"type": "string"},
							"purpose":    map[string]string{"type": "string"},
							"keyPoints":  map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
							"visualHint": map[string]string{"type": "string"},
						},
						"required": []string{"id", "title", "purpose", "keyPoints", "visualHint"},
					},
				},
			},
			"required": []string{"title", "theme", "audience", "style", "slides"},
		},
	}
}

type showSVGToolInput struct {
	SlideID   string `json:"slideId"`
	Title     string `json:"title"`
	SVGBase64 string `json:"svgBase64"`
}

func (s *Server) generateSlideSVG(r *http.Request, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline) (string, error) {
	prompt, err := ppt.BuildSlideSVGPrompt(outline, slide, settings.SupportsTools)
	if err != nil {
		return "", err
	}
	req := llm.GenerateRequest{
		Config:       roleModelConfig(settings.ModelConfig),
		SystemPrompt: settings.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
	}
	if debugEnabled.Load() {
		log.Printf("llm role request request_id=%s role=svg slide_id=%s provider=%s model=%s base_url=%s prompt_bytes=%d supports_tools=%t", requestIDFromContext(r.Context()), slide.ID, settings.ModelConfig.Provider, settings.ModelConfig.Model, settings.ModelConfig.BaseURL, len(prompt), settings.SupportsTools)
	}
	if settings.SupportsTools {
		req.Tools = []llm.ToolDefinition{showSVGToolDefinition()}
		req.ToolChoice = "show_svg"
	}
	resp, err := llm.Generate(r.Context(), req)
	if err != nil {
		return "", err
	}
	if settings.SupportsTools {
		if resp.ToolName != "show_svg" {
			return "", errors.New("模型没有调用 show_svg 工具")
		}
		var input showSVGToolInput
		if err := json.Unmarshal(resp.ToolInput, &input); err != nil {
			return "", err
		}
		svgBytes, err := base64.StdEncoding.DecodeString(input.SVGBase64)
		if err != nil {
			return "", err
		}
		svg := string(svgBytes)
		if err := ppt.ValidateSVG(svg); err != nil {
			return "", err
		}
		return svg, nil
	}
	return ppt.ExtractSVG(resp.Text)
}

func showSVGToolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "show_svg",
		Description: "Return one generated PPT slide SVG as base64 encoded complete SVG document.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"slideId":   map[string]string{"type": "string"},
				"title":     map[string]string{"type": "string"},
				"svgBase64": map[string]string{"type": "string", "description": "Base64 encoded complete SVG document."},
			},
			"required": []string{"slideId", "title", "svgBase64"},
		},
	}
}

func (s *Server) promptSettings(r *http.Request) (store.PromptSettings, error) {
	settings, err := s.store.GetPromptSettings(r.Context())
	if err != nil {
		return store.PromptSettings{}, err
	}
	if settings.Architect.SystemPrompt == "" {
		settings.Architect.SystemPrompt = settings.ArchitectSystemPrompt
	}
	if settings.SVG.SystemPrompt == "" {
		settings.SVG.SystemPrompt = settings.SVGSystemPrompt
	}
	if settings.Architect.SystemPrompt == "" {
		settings.Architect.SystemPrompt = ppt.DefaultArchitectSystemPrompt
	}
	if settings.SVG.SystemPrompt == "" {
		settings.SVG.SystemPrompt = ppt.DefaultSVGSystemPrompt
	}
	return settings, nil
}

func roleModelConfig(config store.ModelConfig) llm.Config {
	return llm.Config{
		Provider: llm.Provider(config.Provider),
		APIKey:   config.APIKey,
		BaseURL:  config.BaseURL,
		Model:    config.Model,
	}
}

func handleGenerateError(w http.ResponseWriter, err error) {
	var userErr llm.UserError
	if errors.As(err, &userErr) {
		writeError(w, http.StatusBadRequest, userErr.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("http json encode error status=%d err=%v", status, err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	detail := ""
	if debugEnabled.Load() {
		detail = message
	}
	requestID := ""
	if requestGetter, ok := w.(interface{ Header() http.Header }); ok {
		requestID = requestGetter.Header().Get("X-Request-ID")
	}
	if debugEnabled.Load() || status >= 500 {
		log.Printf("http error request_id=%s status=%d error=%s", requestID, status, message)
	}
	writeJSON(w, status, errorResponse{Error: message, Detail: detail, RequestID: requestID})
}
