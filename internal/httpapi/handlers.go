package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

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
	quota, err := s.dataStore().CommitDailyQuota(r.Context(), reservation, len(response.Slides))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	releaseReservation = false
	response.Quota = quota
	writeJSON(w, http.StatusOK, response)
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
