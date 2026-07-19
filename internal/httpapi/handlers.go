package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/jobs"
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/service/generation"
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

	svc := s.generationService()
	settings, err := svc.PromptSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp, err := svc.GenerateOutline(r.Context(), settings.Architect, generation.OutlineInput{
		Topic:      input.Topic,
		Audience:   input.Audience,
		SlideCount: input.SlideCount,
		Style:      input.Style,
	})
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

	user, _ := s.currentUser(r)
	reservation, err := s.dataStore().ReserveDailyQuota(r.Context(), store.ReserveQuotaInput{UserID: user.ID, Date: store.TodayUTC(), Slides: len(input.Outline.Slides)})
	if err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日生成额度不足")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	releaseReservation := true
	defer func() {
		if releaseReservation {
			_ = s.dataStore().ReleaseDailyQuota(r.Context(), reservation)
		}
	}()

	svc := s.generationService()
	settings, err := svc.PromptSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	concurrency, source, groupID, groupName, err := svc.EffectiveSlideConcurrency(r.Context(), user, len(input.Outline.Slides))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if debugEnabled.Load() {
		log.Printf("svg generation concurrency request_id=%s total=%d concurrency=%d source=%s user_id=%s group_id=%s group_name=%q", requestIDFromContext(r.Context()), len(input.Outline.Slides), concurrency, source, user.ID, groupID, groupName)
	}

	response := ppt.SVGResponse{Slides: make([]ppt.SlideSVG, len(input.Outline.Slides))}
	if err := svc.GenerateSlideSVGs(r.Context(), settings.SVG, input.Outline, response.Slides, concurrency); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	success, failed := countSlideResults(response.Slides)
	response.Failed = failed
	// Commit only successfully generated pages; release reserved remainder via Commit semantics (uses actualSlides).
	quota, err := s.dataStore().CommitDailyQuota(r.Context(), reservation, success)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	releaseReservation = false
	response.Quota = quota
	writeJSON(w, http.StatusOK, response)
}

func countSlideResults(slides []ppt.SlideSVG) (success, failed int) {
	for _, slide := range slides {
		if slide.Error != "" || strings.TrimSpace(slide.SVG) == "" {
			failed++
			continue
		}
		success++
	}
	return success, failed
}

// handleGenerateOneSVG regenerates a single failed/missing page without stopping others.
func (s *Server) handleGenerateOneSVG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Outline     ppt.PresentationOutline `json:"outline"`
		SlideID     string                  `json:"slideId"`
		Instruction string                  `json:"instruction"`
		CurrentSVG  string                  `json:"currentSvg"`
		JobID       string                  `json:"jobId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if input.SlideID == "" || len(input.Outline.Slides) == 0 {
		writeError(w, http.StatusBadRequest, "请提供 outline 和 slideId")
		return
	}
	var target *ppt.SlideOutline
	for i := range input.Outline.Slides {
		if input.Outline.Slides[i].ID == input.SlideID {
			target = &input.Outline.Slides[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusBadRequest, "未找到对应页面: "+input.SlideID)
		return
	}
	user, _ := s.currentUser(r)
	reservation, err := s.dataStore().ReserveDailyQuota(r.Context(), store.ReserveQuotaInput{UserID: user.ID, Date: store.TodayUTC(), Slides: 1})
	if err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日生成额度不足")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	release := true
	defer func() {
		if release {
			_ = s.dataStore().ReleaseDailyQuota(r.Context(), reservation)
		}
	}()
	svc := s.generationService()
	settings, err := svc.PromptSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var slide ppt.SlideSVG
	if strings.TrimSpace(input.Instruction) != "" {
		slide, err = svc.ReviseOneSlideSVG(r.Context(), settings.SVG, input.Outline, *target, input.CurrentSVG, input.Instruction)
	} else {
		slide, err = svc.GenerateOneSlideSVG(r.Context(), settings.SVG, input.Outline, *target)
	}
	if err != nil {
		// still return slide with error for UI, but keep reservation released
		if strings.TrimSpace(input.JobID) != "" {
			s.patchSVGJobSlide(user.ID, input.JobID, slide, input.Outline)
		}
		writeJSON(w, http.StatusOK, map[string]any{"slide": slide, "failed": true, "jobId": input.JobID})
		return
	}
	quota, err := s.dataStore().CommitDailyQuota(r.Context(), reservation, 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	release = false
	if strings.TrimSpace(input.JobID) != "" {
		if updated, patchErr := s.patchSVGJobSlide(user.ID, input.JobID, slide, input.Outline); patchErr == nil && updated != nil {
			writeJSON(w, http.StatusOK, map[string]any{"slide": slide, "quota": quota, "jobId": input.JobID, "job": updated})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"slide": slide, "quota": quota, "jobId": input.JobID})
}

func (s *Server) patchSVGJobSlide(userID, jobID string, slide ppt.SlideSVG, outline ppt.PresentationOutline) (*jobs.Job, error) {
	s.ensureJobs()
	slideMap := map[string]any{
		"slideId": slide.SlideID,
		"title":   slide.Title,
		"svg":     slide.SVG,
	}
	if slide.Error != "" {
		slideMap["error"] = slide.Error
	}
	return s.jobs.PatchSVGSlide(userID, jobID, slideMap, outline)
}

func (s *Server) generationService() *generation.Service {
	return &generation.Service{
		Store:     s.dataStore(),
		Debug:     func() bool { return debugEnabled.Load() },
		RequestID: requestIDFromContext,
	}
}

func (s *Server) promptSettings(r *http.Request) (store.PromptSettings, error) {
	return s.generationService().PromptSettings(r.Context())
}

func handleGenerateError(w http.ResponseWriter, err error) {
	var userErr llm.UserError
	if errors.As(err, &userErr) {
		writeError(w, http.StatusBadRequest, userErr.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}
