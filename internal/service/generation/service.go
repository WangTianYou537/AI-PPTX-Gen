package generation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	extra, err := domain.ParseRequestJSON(settings.RequestJSON)
	if err != nil {
		return ppt.PresentationOutline{}, err
	}
	req := llm.GenerateRequest{
		Config:       roleModelConfig(settings.ModelConfig),
		SystemPrompt: settings.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
		JSONMode:     !settings.SupportsTools,
		Extra:        extra,
	}
	if settings.SupportsTools {
		req.Tools = []llm.ToolDefinition{createOutlineToolDefinition()}
		req.ToolChoice = "create_outline"
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
			svg, err := s.generateSlideSVG(ctx, settings, outline, job.slide)
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

	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = errors.New(result.slide.ID + " 生成失败: " + result.err.Error())
				cancel()
			}
			continue
		}
		slides[result.index] = ppt.SlideSVG{SlideID: result.slide.ID, Title: result.slide.Title, SVG: result.svg}
	}
	return firstErr
}

func (s *Service) generateSlideSVG(ctx context.Context, settings store.GenerationRoleSettings, outline ppt.PresentationOutline, slide ppt.SlideOutline) (string, error) {
	var err error
	settings, err = s.ResolveRoleSettings(ctx, settings)
	if err != nil {
		return "", err
	}
	prompt, err := ppt.BuildSlideSVGPrompt(outline, slide, settings.SupportsTools)
	if err != nil {
		return "", err
	}
	extra, err := domain.ParseRequestJSON(settings.RequestJSON)
	if err != nil {
		return "", err
	}
	req := llm.GenerateRequest{
		Config:       roleModelConfig(settings.ModelConfig),
		SystemPrompt: settings.SystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: prompt}},
		Extra:        extra,
	}
	if s.debug() {
		log.Printf("llm role request request_id=%s role=svg slide_id=%s provider=%s model=%s base_url=%s prompt_bytes=%d supports_tools=%t", s.requestID(ctx), slide.ID, settings.ModelConfig.Provider, settings.ModelConfig.Model, settings.ModelConfig.BaseURL, len(prompt), settings.SupportsTools)
	}
	if settings.SupportsTools {
		req.Tools = []llm.ToolDefinition{showSVGToolDefinition()}
		req.ToolChoice = "show_svg"
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
	}
	return role, nil
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
