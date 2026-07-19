package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/jobs"
	"wty5.cn/ppt-gen/internal/ppt"
	agentsvc "wty5.cn/ppt-gen/internal/service/agent"
	"wty5.cn/ppt-gen/internal/service/generation"
	"wty5.cn/ppt-gen/internal/store"
)

type createOutlineJobRequest struct {
	Topic      string   `json:"topic"`
	Audience   string   `json:"audience"`
	SlideCount int      `json:"slideCount"`
	Style      string   `json:"style"`
	Notes      string   `json:"notes"`
	UploadIDs  []string `json:"uploadIds"`
}

type createSVGJobRequest struct {
	Outline     ppt.PresentationOutline `json:"outline"`
	SlideIDs    []string                `json:"slideIds"`
	ParentJobID string                  `json:"parentJobId"`
	// ExistingSlides is used for partial retry so final result keeps successful pages.
	ExistingSlides []ppt.SlideSVG `json:"existingSlides"`
	// Optional AI revision for a single slide (requires exactly one slideId).
	Instruction string `json:"instruction"`
	CurrentSVG  string `json:"currentSvg"`
}

type outlineJobPayload struct {
	Input     generation.OutlineInput
	Notes     string
	UploadIDs []string
}

type svgJobPayload struct {
	Outline        ppt.PresentationOutline
	Reservation    store.QuotaReservation
	SlideIDs       []string
	ParentJobID    string
	ExistingSlides []ppt.SlideSVG
	Instruction    string
	CurrentSVG     string
}

func (s *Server) ensureJobs() {
	if s.jobs != nil {
		// keep store pointer fresh for storage switches
		s.jobs.SetStore(s.dataStore())
		return
	}
	s.jobs = jobs.NewManager(jobs.Options{Workers: 2, Store: s.dataStore()}, s.runOutlineJob, s.runSVGJob)
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
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	// /api/jobs/{id}/outline
	if strings.HasSuffix(path, "/outline") {
		s.handlePatchOutlineJob(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := path
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
	job, err := s.jobs.EnqueueOutline(user.ID, outlineJobPayload{
		Input: generation.OutlineInput{
			Topic:      input.Topic,
			Audience:   input.Audience,
			SlideCount: input.SlideCount,
			Style:      input.Style,
		},
		Notes:     input.Notes,
		UploadIDs: input.UploadIDs,
	})
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
	slideIDs := normalizeSlideIDs(input.SlideIDs)
	if len(slideIDs) > 0 {
		// validate ids exist in outline
		known := map[string]bool{}
		for _, sld := range input.Outline.Slides {
			known[sld.ID] = true
		}
		for _, id := range slideIDs {
			if !known[id] {
				writeError(w, http.StatusBadRequest, "大纲中不存在页面: "+id)
				return
			}
		}
	}
	if strings.TrimSpace(input.Instruction) != "" && len(slideIDs) != 1 {
		writeError(w, http.StatusBadRequest, "AI 修改一次只能针对一个页面")
		return
	}
	reserveCount := len(input.Outline.Slides)
	if len(slideIDs) > 0 {
		reserveCount = len(slideIDs)
	}
	user, _ := s.currentUser(r)
	reservation, err := s.dataStore().ReserveDailyQuota(r.Context(), store.ReserveQuotaInput{
		UserID: user.ID,
		Date:   store.TodayUTC(),
		Slides: reserveCount,
	})
	if err != nil {
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日生成额度不足")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	payload := svgJobPayload{
		Outline:        input.Outline,
		Reservation:    reservation,
		SlideIDs:       slideIDs,
		ParentJobID:    strings.TrimSpace(input.ParentJobID),
		ExistingSlides: input.ExistingSlides,
		Instruction:    strings.TrimSpace(input.Instruction),
		CurrentSVG:     input.CurrentSVG,
	}
	var job *jobs.Job
	if payload.ParentJobID != "" {
		label := buildSVGChildLabel(payload)
		job, err = s.jobs.EnqueueSVGChild(user.ID, payload.ParentJobID, label, payload)
	} else {
		job, err = s.jobs.EnqueueSVG(user.ID, payload)
	}
	if err != nil {
		_ = s.dataStore().ReleaseDailyQuota(r.Context(), reservation)
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func normalizeSlideIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *Server) runOutlineJob(ctx context.Context, job *jobs.Job) (json.RawMessage, error) {
	raw, ok := s.jobs.Payload(job.ID)
	if !ok {
		return nil, errors.New("任务参数已失效")
	}
	var payload outlineJobPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("任务参数类型错误")
	}
	files := make([]agentsvc.FileRef, 0, len(payload.UploadIDs))
	for _, id := range payload.UploadIDs {
		up, err := s.dataStore().GetUpload(ctx, id)
		if err != nil {
			continue
		}
		if up.UserID != job.UserID {
			continue
		}
		files = append(files, agentsvc.FileRef{
			UploadID:    up.ID,
			Filename:    up.Filename,
			ContentType: up.ContentType,
			Path:        up.Path,
		})
	}
	runner := &agentsvc.Runner{
		Store: s.dataStore(),
		Gen:   s.generationService(),
		Debug: func() bool { return debugEnabled.Load() },
	}
	result, err := runner.GenerateOutline(ctx, agentsvc.OutlineAgentInput{
		Topic:      payload.Input.Topic,
		Audience:   payload.Input.Audience,
		SlideCount: payload.Input.SlideCount,
		Style:      payload.Input.Style,
		Notes:      payload.Notes,
		Files:      files,
	})
	if err != nil {
		// still return traces if any
		if len(result.Traces) > 0 {
			b, _ := json.Marshal(map[string]any{"traces": result.Traces, "context": result.Context})
			return b, err
		}
		return nil, err
	}
	return json.Marshal(map[string]any{
		"outline": result.Outline,
		"traces":  result.Traces,
		"context": result.Context,
	})
}

func (s *Server) runSVGJob(ctx context.Context, job *jobs.Job) (json.RawMessage, error) {
	raw, ok := s.jobs.Payload(job.ID)
	if !ok {
		return nil, errors.New("任务参数已失效")
	}
	var payload svgJobPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
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

	// Build working slide list: start from existing slides (if any), else empty placeholders for full outline.
	slides := mergeExistingSlides(payload.Outline, payload.ExistingSlides)
	targetIDs := payload.SlideIDs
	if len(targetIDs) == 0 {
		targetIDs = make([]string, 0, len(payload.Outline.Slides))
		for _, sld := range payload.Outline.Slides {
			targetIDs = append(targetIDs, sld.ID)
		}
	}

	// Single-slide AI revision path.
	if payload.Instruction != "" && len(targetIDs) == 1 {
		slideID := targetIDs[0]
		target, ok := findOutlineSlide(payload.Outline, slideID)
		if !ok {
			return nil, errors.New("大纲中不存在页面: " + slideID)
		}
		out, genErr := svc.ReviseOneSlideSVG(ctx, settings.SVG, payload.Outline, target, payload.CurrentSVG, payload.Instruction)
		if genErr != nil {
			out = ppt.SlideSVG{SlideID: target.ID, Title: target.Title, Error: genErr.Error()}
		}
		slides = upsertSlide(slides, out)
		success, failed := countSlideResultsByIDs(slides, targetIDs)
		if success == 0 {
			result, _ := json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failed})
			s.syncParentSVGJob(job.UserID, payload.ParentJobID, slides, payload.Outline)
			if genErr != nil {
				return result, genErr
			}
			return result, errors.New(slideID + " 修改失败")
		}
		quota, err := s.dataStore().CommitDailyQuota(ctx, payload.Reservation, success)
		if err != nil {
			result, _ := json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failed})
			return result, err
		}
		released = true
		s.syncParentSVGJob(job.UserID, payload.ParentJobID, slides, payload.Outline)
		return json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failed, Quota: quota})
	}

	// Partial or full regenerate path.
	// Generate only selected slides by building a temporary outline subset, then map back.
	subOutline := payload.Outline
	if len(payload.SlideIDs) > 0 {
		subSlides := make([]ppt.SlideOutline, 0, len(payload.SlideIDs))
		for _, id := range payload.SlideIDs {
			if sld, ok := findOutlineSlide(payload.Outline, id); ok {
				subSlides = append(subSlides, sld)
			}
		}
		subOutline.Slides = subSlides
	}
	if len(subOutline.Slides) == 0 {
		return nil, errors.New("没有可生成的页面")
	}
	concurrency, _, _, _, err := svc.EffectiveSlideConcurrency(ctx, user, len(subOutline.Slides))
	if err != nil {
		return nil, err
	}
	generated := make([]ppt.SlideSVG, len(subOutline.Slides))
	genErr := svc.GenerateSlideSVGs(ctx, settings.SVG, subOutline, generated, concurrency)
	for _, g := range generated {
		if g.SlideID == "" {
			continue
		}
		slides = upsertSlide(slides, g)
	}
	// Count success only among targeted pages for quota commit.
	success, failedTargets := countSlideResultsByIDs(slides, targetIDs)
	_, failedAll := countSlideResults(slides)

	if success == 0 {
		result, _ := json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failedAll})
		s.syncParentSVGJob(job.UserID, payload.ParentJobID, slides, payload.Outline)
		if genErr != nil {
			return result, genErr
		}
		return result, errors.New("目标页面全部生成失败")
	}
	quota, err := s.dataStore().CommitDailyQuota(ctx, payload.Reservation, success)
	if err != nil {
		result, _ := json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failedAll})
		return result, err
	}
	released = true
	s.syncParentSVGJob(job.UserID, payload.ParentJobID, slides, payload.Outline)
	_ = failedTargets
	return json.Marshal(jobs.SVGResult{Slides: slides, Outline: payload.Outline, Failed: failedAll, Quota: quota})
}

func findOutlineSlide(outline ppt.PresentationOutline, id string) (ppt.SlideOutline, bool) {
	for _, s := range outline.Slides {
		if s.ID == id {
			return s, true
		}
	}
	return ppt.SlideOutline{}, false
}

func mergeExistingSlides(outline ppt.PresentationOutline, existing []ppt.SlideSVG) []ppt.SlideSVG {
	byID := map[string]ppt.SlideSVG{}
	for _, s := range existing {
		if s.SlideID != "" {
			byID[s.SlideID] = s
		}
	}
	out := make([]ppt.SlideSVG, 0, len(outline.Slides))
	for _, s := range outline.Slides {
		if prev, ok := byID[s.ID]; ok {
			// keep previous content; ensure title/id
			if prev.Title == "" {
				prev.Title = s.Title
			}
			if prev.SlideID == "" {
				prev.SlideID = s.ID
			}
			out = append(out, prev)
			continue
		}
		out = append(out, ppt.SlideSVG{SlideID: s.ID, Title: s.Title, Error: "尚未生成"})
	}
	return out
}

func upsertSlide(slides []ppt.SlideSVG, next ppt.SlideSVG) []ppt.SlideSVG {
	for i := range slides {
		if slides[i].SlideID == next.SlideID {
			slides[i] = next
			return slides
		}
	}
	return append(slides, next)
}

func countSlideResultsByIDs(slides []ppt.SlideSVG, ids []string) (success, failed int) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, slide := range slides {
		if !want[slide.SlideID] {
			continue
		}
		if slide.Error != "" || strings.TrimSpace(slide.SVG) == "" {
			failed++
		} else {
			success++
		}
	}
	return success, failed
}

func (s *Server) syncParentSVGJob(userID, parentJobID string, slides []ppt.SlideSVG, outline ppt.PresentationOutline) {
	parentJobID = strings.TrimSpace(parentJobID)
	if parentJobID == "" {
		return
	}
	s.ensureJobs()
	items := make([]any, 0, len(slides))
	for _, slide := range slides {
		m := map[string]any{
			"slideId": slide.SlideID,
			"title":   slide.Title,
			"svg":     slide.SVG,
		}
		if slide.Error != "" {
			m["error"] = slide.Error
		}
		items = append(items, m)
	}
	_, _ = s.jobs.PatchSVGSlides(userID, parentJobID, items, outline)
}

type patchOutlineJobRequest struct {
	Outline ppt.PresentationOutline `json:"outline"`
}

func (s *Server) handlePatchOutlineJob(w http.ResponseWriter, r *http.Request) {
	s.ensureJobs()
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	id = strings.TrimSuffix(id, "/outline")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	var input patchOutlineJobRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if strings.TrimSpace(input.Outline.Title) == "" || len(input.Outline.Slides) == 0 {
		writeError(w, http.StatusBadRequest, "大纲必须包含 title 和 slides")
		return
	}
	user, _ := s.currentUser(r)
	job, err := s.jobs.PatchOutlineResult(user.ID, id, input.Outline)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		if errors.Is(err, jobs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "无权修改该任务")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func buildSVGChildLabel(payload svgJobPayload) string {
	if strings.TrimSpace(payload.Instruction) != "" {
		id := ""
		if len(payload.SlideIDs) == 1 {
			id = payload.SlideIDs[0]
		}
		if id != "" {
			return "AI修改 " + id
		}
		return "AI修改页面"
	}
	if len(payload.SlideIDs) == 1 {
		return "重试 " + payload.SlideIDs[0]
	}
	if len(payload.SlideIDs) > 1 {
		return "批量重试失败页(" + itoa(len(payload.SlideIDs)) + ")"
	}
	return "页面重试"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
