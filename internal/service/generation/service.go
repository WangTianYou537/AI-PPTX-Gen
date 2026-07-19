package generation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"wty5.cn/ppt-gen/internal/domain"
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/store"
)

// Service orchestrates outline and SVG generation without HTTP concerns.
type Service struct {
	Store store.Store
	// Debug enables verbose generation logs.
	Debug func() bool
	// RequestID extracts a request id from context for logs.
	RequestID func(context.Context) string
}

type OutlineInput struct {
	Topic      string
	Audience   string
	SlideCount int
	Style      string
}

func (s *Service) PromptSettings(ctx context.Context) (store.PromptSettings, error) {
	settings, err := s.Store.GetPromptSettings(ctx)
	if err != nil {
		return store.PromptSettings{}, err
	}
	settings.Architect.SystemPrompt, settings.SVG.SystemPrompt = domain.NormalizePromptSettings(
		settings.Architect.SystemPrompt,
		settings.SVG.SystemPrompt,
		settings.ArchitectSystemPrompt,
		settings.SVGSystemPrompt,
	)
	if strings.TrimSpace(settings.Theme.SystemPrompt) == "" {
		settings.Theme.SystemPrompt = ppt.DefaultThemeSystemPrompt
	}
	return settings, nil
}

func (s *Service) EffectiveSlideConcurrency(ctx context.Context, user store.User, slideCount int) (limit int, source, groupID, groupName string, err error) {
	settings, err := s.Store.GetSystemSettings(ctx)
	if err != nil {
		return 0, "", "", "", err
	}
	group, err := s.Store.GetUserGroup(ctx, user.GroupID)
	if err != nil {
		group, err = s.Store.GetUserGroup(ctx, settings.DefaultUserGroupID)
		if err != nil {
			group, err = s.Store.GetUserGroup(ctx, store.DefaultUserGroupID)
			if err != nil {
				return 0, "", "", "", err
			}
		}
	}
	limit, source = domain.ResolveSlideConcurrency(user.SlideConcurrencyLimit, group.SlideConcurrencyLimit, settings.DefaultSlideConcurrencyLimit, slideCount)
	return limit, source, group.ID, group.Name, nil
}

func (s *Service) GenerateOutline(ctx context.Context, settings store.GenerationRoleSettings, input OutlineInput) (ppt.PresentationOutline, error) {
	var err error
	settings, err = s.ResolveRoleSettings(ctx, settings)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	prompt := ppt.BuildArchitectPrompt(input.Topic, input.Audience, input.Style, input.SlideCount)
	if !settings.SupportsTools {
		prompt += "\n\n输出强制要求：你的回复第一个字符必须是 {，最后一个字符必须是 }。禁止输出任何 JSON 之外的文字。"
	}
	var tools []llm.ToolDefinition
	toolChoice := ""
	if settings.SupportsTools {
		tools = []llm.ToolDefinition{createOutlineToolDefinition()}
		toolChoice = "create_outline"
	}
	req, err := s.buildGenerateRequest(settings, prompt, !settings.SupportsTools, tools, toolChoice)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	resp, err := llm.Generate(ctx, req)
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

func (s *Service) GenerateSlideSVGs(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slides []ppt.SlideSVG, concurrency int) error {
	// Plan a deck-wide palette once, then reuse for every page to keep style consistent.
	if lock, err := s.PlanThemeColors(ctx, outline); err == nil {
		outline = ppt.WithVisualDesignLock(outline, lock)
	}
	// Do not cancel remaining work on a single slide failure; continue other pages.
	type slideJob struct {
		index int
		slide ppt.SlideOutline
	}
	type slideResult struct {
		index int
		slide ppt.SlideOutline
		svg   string
		err   error
	}

	jobs := make(chan slideJob)
	results := make(chan slideResult)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for job := range jobs {
			started := time.Now()
			if s.debug() {
				log.Printf("svg slide generation start request_id=%s index=%d total=%d slide_id=%s title=%q provider=%s model=%s", s.requestID(ctx), job.index+1, len(outline.Slides), job.slide.ID, job.slide.Title, settings.ModelConfig.Provider, settings.ModelConfig.Model)
			}
			svg, err := s.generateSlideSVG(ctx, settings, outline, job.slide, "", "")
			if err != nil {
				if s.debug() {
					log.Printf("svg slide generation failed request_id=%s index=%d total=%d slide_id=%s duration=%s err=%v", s.requestID(ctx), job.index+1, len(outline.Slides), job.slide.ID, time.Since(started), err)
				}
				results <- slideResult{index: job.index, slide: job.slide, err: err}
				continue
			}
			if s.debug() {
				log.Printf("svg slide generation complete request_id=%s index=%d total=%d slide_id=%s duration=%s svg_bytes=%d", s.requestID(ctx), job.index+1, len(outline.Slides), job.slide.ID, time.Since(started), len(svg))
			}
			results <- slideResult{index: job.index, slide: job.slide, svg: svg}
		}
	}
	if concurrency < 1 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker()
	}
	go func() {
		defer close(jobs)
		for index, slide := range outline.Slides {
			select {
			case <-ctx.Done():
				return
			case jobs <- slideJob{index: index, slide: slide}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	failed := 0
	for result := range results {
		if result.err != nil {
			failed++
			slides[result.index] = ppt.SlideSVG{
				SlideID: result.slide.ID,
				Title:   result.slide.Title,
				Error:   result.err.Error(),
			}
			continue
		}
		slides[result.index] = ppt.SlideSVG{SlideID: result.slide.ID, Title: result.slide.Title, SVG: result.svg}
	}
	if ctx.Err() != nil && failed == len(outline.Slides) {
		return ctx.Err()
	}
	// Partial failures are represented on each slide; only fail hard when nothing succeeded.
	if failed == len(outline.Slides) && len(outline.Slides) > 0 {
		return errors.New(summarizeAllSlideFailures(slides))
	}
	return nil
}

func summarizeAllSlideFailures(slides []ppt.SlideSVG) string {
	if len(slides) == 0 {
		return "全部页面生成失败"
	}
	var b strings.Builder
	b.WriteString("全部页面生成失败（")
	b.WriteString(fmt.Sprintf("%d/%d", len(slides), len(slides)))
	b.WriteString("）：")
	limit := 5
	shown := 0
	for _, slide := range slides {
		if slide.Error == "" {
			continue
		}
		if shown > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(slide.SlideID)
		if slide.Title != "" {
			b.WriteString("(")
			b.WriteString(slide.Title)
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(slide.Error)
		shown++
		if shown >= limit {
			remain := 0
			for _, s := range slides {
				if s.Error != "" {
					remain++
				}
			}
			remain -= shown
			if remain > 0 {
				b.WriteString(fmt.Sprintf(" …另有 %d 页失败", remain))
			}
			break
		}
	}
	if shown == 0 {
		return "全部页面生成失败"
	}
	return b.String()
}

// GenerateOneSlideSVG regenerates a single slide page.
func (s *Service) GenerateOneSlideSVG(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline) (ppt.SlideSVG, error) {
	return s.GenerateOrReviseOneSlideSVG(ctx, settings, outline, slide, "", "")
}

// ReviseOneSlideSVG revises an existing slide SVG using natural-language instruction.
func (s *Service) ReviseOneSlideSVG(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline, currentSVG, instruction string) (ppt.SlideSVG, error) {
	return s.GenerateOrReviseOneSlideSVG(ctx, settings, outline, slide, currentSVG, instruction)
}

// GenerateOrReviseOneSlideSVG generates or revises a single slide.
// If instruction is empty, it regenerates from outline; otherwise revises currentSVG.
func (s *Service) GenerateOrReviseOneSlideSVG(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline, currentSVG, instruction string) (ppt.SlideSVG, error) {
	if lock, err := s.PlanThemeColors(ctx, outline); err == nil {
		outline = ppt.WithVisualDesignLock(outline, lock)
	}
	svg, err := s.generateSlideSVG(ctx, settings, outline, slide, currentSVG, instruction)
	if err != nil {
		return ppt.SlideSVG{SlideID: slide.ID, Title: slide.Title, Error: err.Error()}, err
	}
	return ppt.SlideSVG{SlideID: slide.ID, Title: slide.Title, SVG: svg}, nil
}

func (s *Service) generateSlideSVG(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline, currentSVG, instruction string) (string, error) {
	var err error
	settings, err = s.ResolveRoleSettings(ctx, settings)
	if err != nil {
		return "", err
	}
	var prompt string
	if strings.TrimSpace(instruction) != "" {
		prompt, err = ppt.BuildSlideSVGRevisionPrompt(outline, slide, currentSVG, strings.TrimSpace(instruction), settings.SupportsTools)
	} else {
		prompt, err = ppt.BuildSlideSVGPrompt(outline, slide, settings.SupportsTools)
	}
	if err != nil {
		return "", err
	}
	var tools []llm.ToolDefinition
	toolChoice := ""
	if settings.SupportsTools {
		tools = []llm.ToolDefinition{showSVGToolDefinition()}
		toolChoice = "show_svg"
	}
	req, err := s.buildGenerateRequest(settings, prompt, false, tools, toolChoice)
	if err != nil {
		return "", err
	}
	if s.debug() {
		log.Printf("llm role request request_id=%s role=svg slide_id=%s provider=%s model=%s base_url=%s prompt_bytes=%d supports_tools=%t", s.requestID(ctx), slide.ID, settings.ModelConfig.Provider, settings.ModelConfig.Model, settings.ModelConfig.BaseURL, len(prompt), settings.SupportsTools)
	}
	resp, err := llm.Generate(ctx, req)
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

func (s *Service) buildGenerateRequest(settings store.GenerationRoleSettings, userPrompt string, jsonMode bool, tools []llm.ToolDefinition, toolChoice string) (llm.GenerateRequest, error) {
	req := llm.GenerateRequest{
		Config:       roleModelConfig(settings.ModelConfig),
		SystemPrompt: settings.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userPrompt}},
		JSONMode:     jsonMode,
		Tools:        tools,
		ToolChoice:   toolChoice,
	}
	extra, err := domain.ParseRequestJSON(settings.RequestJSON)
	if err != nil {
		return llm.GenerateRequest{}, err
	}
	// Incremental fields are merged into each provider's default request body.
	req.Extra = extra
	return req, nil
}

func (s *Service) ResolveRoleSettings(ctx context.Context, role store.GenerationRoleSettings) (store.GenerationRoleSettings, error) {
	if strings.TrimSpace(role.ProviderID) == "" {
		return store.GenerationRoleSettings{}, errors.New("请先为角色绑定 LLM 提供商")
	}
	provider, err := s.Store.GetLLMProvider(ctx, role.ProviderID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.GenerationRoleSettings{}, errors.New("所选 LLM 提供商不存在")
		}
		return store.GenerationRoleSettings{}, err
	}
	if !provider.Enabled {
		return store.GenerationRoleSettings{}, errors.New("所选 LLM 提供商已禁用")
	}
	model := strings.TrimSpace(role.Model)
	if model == "" {
		return store.GenerationRoleSettings{}, errors.New("请为角色选择模型")
	}
	role.ModelConfig = store.ModelConfig{
		Provider: provider.Kind,
		APIKey:   provider.APIKey,
		BaseURL:  provider.BaseURL,
		Model:    model,
		Proxy:    provider.Proxy,
	}
	return role, nil
}

func (s *Service) PlanThemeColors(ctx context.Context, outline ppt.PresentationOutline) (ppt.VisualDesignLock, error) {
	// Prefer dedicated theme role; fall back to heuristic lock.
	fallback := ppt.ResolveVisualDesignLock(outline.Style)
	settings, err := s.PromptSettings(ctx)
	if err != nil {
		return fallback, err
	}
	role := settings.Theme
	// If theme role has no provider, inherit SVG role provider for convenience.
	if strings.TrimSpace(role.ProviderID) == "" {
		role.ProviderID = settings.SVG.ProviderID
		if strings.TrimSpace(role.Model) == "" {
			role.Model = settings.SVG.Model
		}
	}
	if strings.TrimSpace(role.ProviderID) == "" {
		return fallback, nil
	}
	if strings.TrimSpace(role.SystemPrompt) == "" {
		role.SystemPrompt = ppt.DefaultThemeSystemPrompt
	}
	role, err = s.ResolveRoleSettings(ctx, role)
	if err != nil {
		return fallback, nil
	}
	prompt := ppt.BuildThemePlannerPrompt(outline.Style, outline.Title, outline.Theme, outline.Audience)
	// Force non-stream JSON object for reliability.
	if strings.TrimSpace(role.RequestJSON) == "" {
		role.RequestJSON = `{"stream":false}`
	}
	req, err := s.buildGenerateRequest(role, prompt, true, nil, "")
	if err != nil {
		return fallback, nil
	}
	resp, err := llm.Generate(ctx, req)
	if err != nil {
		if s.debug() {
			log.Printf("theme planner failed request_id=%s err=%v", s.requestID(ctx), err)
		}
		return fallback, nil
	}
	text := strings.TrimSpace(resp.Text)
	if text == "" && len(resp.ToolInput) > 0 {
		text = string(resp.ToolInput)
	}
	lock, err := ppt.ParseVisualDesignLockJSON(text)
	if err != nil {
		if s.debug() {
			log.Printf("theme planner parse failed request_id=%s err=%v text=%q", s.requestID(ctx), err, text)
		}
		return fallback, nil
	}
	if s.debug() {
		log.Printf("theme planner ok request_id=%s mode=%s bg=%s accent=%s", s.requestID(ctx), lock.Mode, lock.Background, lock.Accent)
	}
	return lock, nil
}

func (s *Service) debug() bool {
	if s.Debug == nil {
		return false
	}
	return s.Debug()
}

func (s *Service) requestID(ctx context.Context) string {
	if s.RequestID == nil {
		return ""
	}
	return s.RequestID(ctx)
}
