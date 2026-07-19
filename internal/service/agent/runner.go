package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"wty5.cn/ppt-gen/internal/agent"
	"wty5.cn/ppt-gen/internal/domain"
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/service/generation"
	"wty5.cn/ppt-gen/internal/store"
)

type Runner struct {
	Store store.Store
	Gen   *generation.Service
	Debug func() bool
}

type FileRef struct {
	UploadID    string
	Filename    string
	ContentType string
	Path        string
}

type OutlineAgentInput struct {
	Topic      string
	Audience   string
	SlideCount int
	Style      string
	Files      []FileRef
	// Optional notes from UI.
	Notes string
}

type StepTrace struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type OutlineAgentResult struct {
	Outline ppt.PresentationOutline `json:"outline"`
	Traces  []StepTrace             `json:"traces,omitempty"`
	Context string                  `json:"context,omitempty"`
}

func (r *Runner) GenerateOutline(ctx context.Context, input OutlineAgentInput) (OutlineAgentResult, error) {
	settings, err := r.Gen.PromptSettings(ctx)
	if err != nil {
		return OutlineAgentResult{}, err
	}
	wf, err := agent.ParseWorkflowJSON(settings.ArchitectWorkflowJSON)
	if err != nil {
		wf = agent.DefaultWorkflow()
	}
	wf = agent.NormalizeWorkflow(wf)

	var (
		visionNotes  string
		searchNotes  string
		summaryNotes string
		traces       []StepTrace
	)

	needsSearch := looksLikeNeedsSearch(input.Topic, input.Notes)
	hasFiles := len(input.Files) > 0

	for _, step := range wf.Steps {
		if !step.Enabled {
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Skipped: true, Reason: "disabled"})
			continue
		}
		if !matchCondition(step.Condition, hasFiles, needsSearch || strings.TrimSpace(visionNotes) != "" && strings.Contains(strings.ToLower(visionNotes), "需要检索")) {
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Skipped: true, Reason: "condition not met: " + step.Condition})
			continue
		}

		role, err := r.resolveStepRole(ctx, settings, step)
		if err != nil {
			// Vision/search optional failures should not hard-stop outline if outline step can still run.
			if step.Kind == agent.StepOutline {
				return OutlineAgentResult{Traces: traces}, err
			}
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Error: err.Error()})
			continue
		}

		switch step.Kind {
		case agent.StepVision:
			out, runErr := r.runVision(ctx, role, input)
			if runErr != nil {
				traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Error: runErr.Error()})
				continue
			}
			visionNotes = out
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Output: trimForTrace(out)})
		case agent.StepSearch:
			out, runErr := r.runSearch(ctx, role, input, visionNotes)
			if runErr != nil {
				traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Error: runErr.Error()})
				continue
			}
			searchNotes = out
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Output: trimForTrace(out)})
		case agent.StepSummarize:
			out, runErr := r.runSummarize(ctx, role, input, visionNotes, searchNotes)
			if runErr != nil {
				traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Error: runErr.Error()})
				continue
			}
			summaryNotes = out
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Output: trimForTrace(out)})
		case agent.StepOutline:
			ctxText := firstNonEmpty(summaryNotes, joinNotes(visionNotes, searchNotes), input.Notes)
			outline, runErr := r.runOutline(ctx, role, settings.Architect, input, ctxText)
			if runErr != nil {
				return OutlineAgentResult{Traces: append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Error: runErr.Error()}), Context: ctxText}, runErr
			}
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Output: "outline generated"})
			return OutlineAgentResult{Outline: outline, Traces: traces, Context: ctxText}, nil
		default:
			traces = append(traces, StepTrace{ID: step.ID, Kind: step.Kind, Name: step.Name, Skipped: true, Reason: "unknown kind"})
		}
	}
	return OutlineAgentResult{Traces: traces}, errors.New("workflow 未配置可用的 outline 步骤")
}

func (r *Runner) resolveStepRole(ctx context.Context, settings store.PromptSettings, step agent.WorkflowStep) (store.GenerationRoleSettings, error) {
	// Prefer step-level provider/model; fallback to architect role.
	role := settings.Architect
	if strings.TrimSpace(step.ProviderID) != "" {
		role.ProviderID = step.ProviderID
	}
	if strings.TrimSpace(step.Model) != "" {
		role.Model = step.Model
	}
	if strings.TrimSpace(step.SystemPrompt) != "" {
		role.SystemPrompt = step.SystemPrompt
	}
	if strings.TrimSpace(step.RequestJSON) != "" {
		role.RequestJSON = step.RequestJSON
	}
	// Vision/search steps enable tools when request JSON asks for tools or kind is search.
	if step.Kind == agent.StepSearch {
		role.SupportsTools = true
	}
	return r.Gen.ResolveRoleSettings(ctx, role)
}

func (r *Runner) runVision(ctx context.Context, role store.GenerationRoleSettings, input OutlineAgentInput) (string, error) {
	if len(input.Files) == 0 {
		return "", nil
	}

	// Multimodal parts: text prompt + inline PDF/images (Gemini official style / OpenAI-compatible file parts).
	const maxFileBytes = 20 << 20 // 20MB hard limit
	const maxFiles = 8
	parts := make([]llm.ContentPart, 0, 1+len(input.Files)*2)

	intro := "请深入分析用户上传的材料（图片/PDF/文本），提取对 PPT 大纲真正有用的信息。\n"
	intro += "主题：" + input.Topic + "\n"
	intro += "受众：" + input.Audience + "\n"
	intro += "风格：" + input.Style + "\n"
	if strings.TrimSpace(input.Notes) != "" {
		intro += "用户备注：" + input.Notes + "\n"
	}
	intro += "\n输出要求：\n"
	intro += "1) 文档/图片核心观点与结构\n"
	intro += "2) 关键数据、结论、图表含义\n"
	intro += "3) 可直接用于 PPT 的要点\n"
	intro += "4) 不确定处明确标注\n"
	intro += "不要只输出 PDF 元数据（作者/DOI/页眉），必须基于正文/页面内容总结。\n"
	parts = append(parts, llm.ContentPart{Type: "text", Text: intro})

	used := 0
	for i, f := range input.Files {
		if used >= maxFiles {
			parts = append(parts, llm.ContentPart{Type: "text", Text: fmt.Sprintf("（其余 %d 个文件已省略，避免请求过大）", len(input.Files)-i)})
			break
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			parts = append(parts, llm.ContentPart{Type: "text", Text: fmt.Sprintf("[文件%s 读取失败: %v]", f.Filename, err)})
			continue
		}
		if int64(len(data)) > maxFileBytes {
			parts = append(parts, llm.ContentPart{Type: "text", Text: fmt.Sprintf("[文件%s 超过 20MB，已跳过]", f.Filename)})
			continue
		}
		mime := strings.TrimSpace(f.ContentType)
		if mime == "" {
			mime = guessMIME(f.Filename)
		}
		lowerName := strings.ToLower(f.Filename)
		isPDF := strings.Contains(strings.ToLower(mime), "pdf") || strings.HasSuffix(lowerName, ".pdf")
		isImage := strings.HasPrefix(strings.ToLower(mime), "image/") || hasImageExt(lowerName)
		isText := strings.HasPrefix(strings.ToLower(mime), "text/") || hasTextExt(lowerName)

		parts = append(parts, llm.ContentPart{Type: "text", Text: fmt.Sprintf("\n[附件%d] %s (%s, %d bytes)\n", i+1, f.Filename, mime, len(data))})
		if isText {
			s := string(data)
			if !utf8.ValidString(s) {
				s = strings.ToValidUTF8(s, "")
			}
			parts = append(parts, llm.ContentPart{Type: "text", Text: trimBytes(s, 20000)})
			used++
			continue
		}
		if isPDF || isImage {
			// Official Gemini style: inline_data with full base64 payload.
			if isPDF && !strings.Contains(strings.ToLower(mime), "pdf") {
				mime = "application/pdf"
			}
			parts = append(parts, llm.ContentPart{
				Type:     "file",
				MIMEType: mime,
				Data:     data,
				Filename: f.Filename,
			})
			used++
			continue
		}
		// Unknown binary: try printable scrape as weak fallback
		parts = append(parts, llm.ContentPart{Type: "text", Text: trimBytes(scrapePrintable(data), 12000)})
		used++
	}

	req := llm.GenerateRequest{
		Config: llm.Config{
			Provider: llm.Provider(role.ModelConfig.Provider),
			APIKey:   role.ModelConfig.APIKey,
			BaseURL:  role.ModelConfig.BaseURL,
			Model:    role.ModelConfig.Model,
			Proxy:    role.ModelConfig.Proxy,
		},
		SystemPrompt: firstNonEmpty(role.SystemPrompt, "你是文档与视觉理解助手，擅长从 PDF 和图片中提取可用于演示文稿的结构化信息。"),
		Messages: []llm.Message{{
			Role:  "user",
			Parts: parts,
		}},
		JSONMode: false,
	}
	extra, err := domain.ParseRequestJSON(role.RequestJSON)
	if err != nil {
		return "", err
	}
	// Vision/document understanding is more reliable non-stream.
	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["stream"]; !ok {
		extra["stream"] = false
	}
	req.Extra = extra

	resp, err := llm.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(firstNonEmpty(resp.Text, string(resp.ToolInput)))
	if text == "" {
		return "", errors.New("视觉理解未返回有效内容")
	}
	return text, nil
}

func guessMIME(filename string) string {
	n := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(n, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(n, ".png"):
		return "image/png"
	case strings.HasSuffix(n, ".jpg"), strings.HasSuffix(n, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(n, ".webp"):
		return "image/webp"
	case strings.HasSuffix(n, ".gif"):
		return "image/gif"
	case strings.HasSuffix(n, ".txt"):
		return "text/plain"
	case strings.HasSuffix(n, ".md"):
		return "text/markdown"
	case strings.HasSuffix(n, ".csv"):
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

func hasImageExt(name string) bool {
	return strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".webp") || strings.HasSuffix(name, ".gif")
}

func hasTextExt(name string) bool {
	return strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".json")
}

func (r *Runner) runSearch(ctx context.Context, role store.GenerationRoleSettings, input OutlineAgentInput, visionNotes string) (string, error) {
	var b strings.Builder
	b.WriteString("请针对以下主题进行信息补充/检索整理：\n")
	b.WriteString("主题：")
	b.WriteString(input.Topic)
	b.WriteString("\n受众：")
	b.WriteString(input.Audience)
	b.WriteString("\n风格：")
	b.WriteString(input.Style)
	if strings.TrimSpace(input.Notes) != "" {
		b.WriteString("\n用户备注：")
		b.WriteString(input.Notes)
	}
	if strings.TrimSpace(visionNotes) != "" {
		b.WriteString("\n上传材料理解：\n")
		b.WriteString(visionNotes)
	}
	b.WriteString("\n输出：关键事实、时间点、争议点、可用于 PPT 的证据要点。")

	// Prefer tools if request JSON contains tools, else plain completion.
	req, err := buildReq(role, b.String(), false)
	if err != nil {
		return "", err
	}
	resp, err := llm.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(firstNonEmpty(resp.Text, string(resp.ToolInput))), nil
}

func (r *Runner) runSummarize(ctx context.Context, role store.GenerationRoleSettings, input OutlineAgentInput, visionNotes, searchNotes string) (string, error) {
	if strings.TrimSpace(visionNotes) == "" && strings.TrimSpace(searchNotes) == "" && strings.TrimSpace(input.Notes) == "" {
		return "", nil
	}
	prompt := "请汇总以下材料为 PPT 架构上下文 Context：\n" +
		"主题：" + input.Topic + "\n受众：" + input.Audience + "\n" +
		"用户备注：\n" + input.Notes + "\n\n" +
		"视觉/文档理解：\n" + visionNotes + "\n\n" +
		"检索补充：\n" + searchNotes + "\n"
	req, err := buildReq(role, prompt, false)
	if err != nil {
		return "", err
	}
	resp, err := llm.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}

func (r *Runner) runOutline(ctx context.Context, stepRole, architectRole store.GenerationRoleSettings, input OutlineAgentInput, contextText string) (ppt.PresentationOutline, error) {
	// Merge: step role provides provider/model; architect prompt used if step prompt empty.
	role := stepRole
	if strings.TrimSpace(role.SystemPrompt) == "" {
		role.SystemPrompt = architectRole.SystemPrompt
	}
	// Keep architect request json if step didn't set one.
	if strings.TrimSpace(role.RequestJSON) == "" {
		role.RequestJSON = architectRole.RequestJSON
	}
	role.SupportsTools = architectRole.SupportsTools

	userPrompt := ppt.BuildArchitectPrompt(input.Topic, input.Audience, input.Style, input.SlideCount)
	if strings.TrimSpace(contextText) != "" {
		userPrompt += "\n\n【Context / 调研与材料摘要】\n" + contextText
	}
	if !role.SupportsTools {
		userPrompt += "\n\n输出强制要求：你的回复第一个字符必须是 {，最后一个字符必须是 }。禁止输出任何 JSON 之外的文字。"
	}
	var tools []llm.ToolDefinition
	toolChoice := ""
	if role.SupportsTools {
		// reuse generation tool via local definition
		tools = []llm.ToolDefinition{{
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
		}}
		toolChoice = "create_outline"
	}
	req, err := buildReqWithTools(role, userPrompt, !role.SupportsTools, tools, toolChoice)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	resp, err := llm.Generate(ctx, req)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	if role.SupportsTools {
		if resp.ToolName != "create_outline" {
			// fall back to text parse if tool not used
			if strings.TrimSpace(resp.Text) != "" {
				return ppt.ParseOutline(resp.Text)
			}
			return ppt.PresentationOutline{}, errors.New("模型没有调用 create_outline 工具")
		}
		return ppt.ParseOutline(string(resp.ToolInput))
	}
	return ppt.ParseOutline(resp.Text)
}

func buildReq(role store.GenerationRoleSettings, userPrompt string, jsonMode bool) (llm.GenerateRequest, error) {
	return buildReqWithTools(role, userPrompt, jsonMode, nil, "")
}

func buildReqWithTools(role store.GenerationRoleSettings, userPrompt string, jsonMode bool, tools []llm.ToolDefinition, toolChoice string) (llm.GenerateRequest, error) {
	req := llm.GenerateRequest{
		Config: llm.Config{
			Provider: llm.Provider(role.ModelConfig.Provider),
			APIKey:   role.ModelConfig.APIKey,
			BaseURL:  role.ModelConfig.BaseURL,
			Model:    role.ModelConfig.Model,
			Proxy:    role.ModelConfig.Proxy,
		},
		SystemPrompt: role.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userPrompt}},
		JSONMode:     jsonMode,
		Tools:        tools,
		ToolChoice:   toolChoice,
	}
	extra, err := domain.ParseRequestJSON(role.RequestJSON)
	if err != nil {
		return llm.GenerateRequest{}, err
	}
	req.Extra = extra
	return req, nil
}

func matchCondition(cond string, hasFiles, needsSearch bool) bool {
	switch strings.ToLower(strings.TrimSpace(cond)) {
	case "", agent.ConditionAlways:
		return true
	case agent.ConditionHasFiles:
		return hasFiles
	case agent.ConditionNeedsSearch:
		return needsSearch
	default:
		return true
	}
}

func looksLikeNeedsSearch(topic, notes string) bool {
	text := strings.ToLower(topic + " " + notes)
	keys := []string{"最新", "实时", "今年", "2024", "2025", "2026", "新闻", "政策", "股价", "行情", "数据", "趋势", "report", "latest", "news", "today", "current"}
	for _, k := range keys {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func extractFileText(f FileRef) (string, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	ct := strings.ToLower(f.ContentType)
	name := strings.ToLower(f.Filename)
	// plain text-like
	if strings.HasPrefix(ct, "text/") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".csv") {
		s := string(data)
		if !utf8.ValidString(s) {
			s = strings.ToValidUTF8(s, "")
		}
		return trimBytes(s, 12000), nil
	}
	// very light PDF text scrape: extract readable ASCII/UTF8 fragments
	if strings.Contains(ct, "pdf") || strings.HasSuffix(name, ".pdf") {
		return trimBytes(scrapePrintable(data), 12000), nil
	}
	// images: no text extract here
	return "", nil
}

func scrapePrintable(data []byte) string {
	var b strings.Builder
	run := 0
	for _, c := range data {
		if c == 9 || c == 10 || c == 13 || (c >= 32 && c < 127) {
			b.WriteByte(c)
			run++
		} else if run > 0 {
			b.WriteByte(' ')
			run = 0
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	return s
}

func trimBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}

func trimForTrace(s string) string {
	return trimBytes(strings.TrimSpace(s), 1500)
}

func joinNotes(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Ensure filepath used for future local path ops.
var _ = filepath.Base
var _ = json.Marshal
