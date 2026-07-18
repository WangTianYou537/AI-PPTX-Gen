package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/jobs"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/service/generation"
	"wty5.cn/ppt-gen/internal/store"
)

type createOutlineJobRequest struct {
	Topic      string `json:"topic"`
	Audience   string `json:"audience"`
	SlideCount int    `json:"slideCount"`
	Style      string `json:"style"`
}

type createSVGJobRequest struct {
	Outline ppt.PresentationOutline `json:"outline"`
}

type outlineJobPayload struct {
	Input generation.OutlineInput
}

type svgJobPayload struct {
	Outline     ppt.PresentationOutline
	Reservation store.QuotaReservation
}

func (s *Server) ensureJobs() {
	if s.jobs != nil {
		return
	}
	s.jobs = jobs.NewManager(jobs.Options{Workers: 2}, s.runOutlineJob, s.runSVGJob)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	s.ensureJobs()
	user, _ := s.currentUser(r)
	switch r.Method {
	case http.MethodGet:
		list := s.jobs.List(user.ID, 30)
		writeJSON(w, http.StatusOK, map[string]any{"jobs": list})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	s.ensureJobs()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if id == "" || strings.Contains(id, "/") || id == "outline" || id == "svg" {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	user, _ := s.currentUser(r)
	job, err := s.jobs.Get(user.ID, id)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		if errors.Is(err, jobs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "无权查看该任务")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCreateOutlineJob(w http.ResponseWriter, r *http.Request) {
	s.ensureJobs()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input createOutlineJobRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if strings.TrimSpace(input.Topic) == "" {
		writeError(w, http.StatusBadRequest, "请填写 PPT 主题")
		return
	}
	user, _ := s.currentUser(r)
	job, err := s.jobs.EnqueueOutline(user.ID, outlineJobPayload{Input: generation.OutlineInput{
		Topic:      input.Topic,
		Audience:   input.Audience,
		SlideCount: input.SlideCount,
		Style:      input.Style,
	}})
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleCreateSVGJob(w http.ResponseWriter, r *http.Request) {
	s.ensureJobs()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input createSVGJobRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if len(input.Outline.Slides) == 0 {
		writeError(w, http.StatusBadRequest, "请先生成并确认 PPT 架构")
		return
	}
	user, _ := s.currentUser(r)
	reservation, err := s.dataStore().ReserveDailyQuota(r.Context(), store.ReserveQuotaInput{
		UserID: user.ID,
		Date:   store.TodayUTC(),
		Slides: len(input.Outline.Slides),
	})
	if err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日生成额度不足")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	job, err := s.jobs.EnqueueSVG(user.ID, svgJobPayload{Outline: input.Outline, Reservation: reservation})
	if err != nil {
		_ = s.dataStore().ReleaseDailyQuota(r.Context(), reservation)
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) runOutlineJob(ctx context.Context, job *jobs.Job) (json.RawMessage, error) {
	payloadAny, ok := s.jobs.Payload(job.ID)
	if !ok {
		return nil, errors.New("任务参数已失效")
	}
	payload, ok := payloadAny.(outlineJobPayload)
	if !ok {
		return nil, errors.New("任务参数类型错误")
	}
	svc := s.generationService()
	settings, err := svc.PromptSettings(ctx)
	if err != nil {
		return nil, err
	}
	outline, err := svc.GenerateOutline(ctx, settings.Architect, payload.Input)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jobs.OutlineResult{Outline: outline})
}

func (s *Server) runSVGJob(ctx context.Context, job *jobs.Job) (json.RawMessage, error) {
	payloadAny, ok := s.jobs.Payload(job.ID)
	if !ok {
		return nil, errors.New("任务参数已失效")
	}
	payload, ok := payloadAny.(svgJobPayload)
	if !ok {
		return nil, errors.New("任务参数类型错误")
	}

	// Always release reservation unless commit succeeds.
	released := false
	defer func() {
		if !released {
			_ = s.dataStore().ReleaseDailyQuota(ctx, payload.Reservation)
		}
	}()

	user, err := s.dataStore().GetUserByID(ctx, job.UserID)
	if err != nil {
		return nil, err
	}
	svc := s.generationService()
	settings, err := svc.PromptSettings(ctx)
	if err != nil {
		return nil, err
	}
	concurrency, _, _, _, err := svc.EffectiveSlideConcurrency(ctx, user, len(payload.Outline.Slides))
	if err != nil {
		return nil, err
	}
	slides := make([]ppt.SlideSVG, len(payload.Outline.Slides))
	if err := svc.GenerateSlideSVGs(ctx, settings.SVG, payload.Outline, slides, concurrency); err != nil {
		return nil, err
	}
	quota, err := s.dataStore().CommitDailyQuota(ctx, payload.Reservation, len(slides))
	if err != nil {
		return nil, err
	}
	released = true
	return json.Marshal(jobs.SVGResult{Slides: slides, Quota: quota})
}
