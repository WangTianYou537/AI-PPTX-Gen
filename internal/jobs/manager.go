package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"wty5.cn/ppt-gen/internal/store"
)

var (
	ErrNotFound  = errors.New("job not found")
	ErrForbidden = errors.New("job access denied")
)

// Manager runs async generation jobs and persists them via store.Store.
// Browser reloads can resume polling; process restart requeues open jobs.
type Manager struct {
	mu      sync.RWMutex
	store   store.Store
	queue   chan string
	workers int
	ttl     time.Duration

	runOutline func(ctx context.Context, job *Job) (json.RawMessage, error)
	runSVG     func(ctx context.Context, job *Job) (json.RawMessage, error)

	// in-memory payload cache for the currently executing process.
	// Payloads are also persisted as JSON for cross-restart recovery.
	payloads map[string]json.RawMessage
	// in-memory status cache for fast Get; source of truth is store.
	jobs map[string]*Job

	stop chan struct{}
	wg   sync.WaitGroup
}

type Options struct {
	Workers int
	TTL     time.Duration
	Store   store.Store
}

func NewManager(opts Options, runOutline func(context.Context, *Job) (json.RawMessage, error), runSVG func(context.Context, *Job) (json.RawMessage, error)) *Manager {
	if opts.Workers < 1 {
		opts.Workers = 2
	}
	if opts.TTL <= 0 {
		opts.TTL = 24 * time.Hour
	}
	m := &Manager{
		store:      opts.Store,
		queue:      make(chan string, 256),
		workers:    opts.Workers,
		ttl:        opts.TTL,
		runOutline: runOutline,
		runSVG:     runSVG,
		payloads:   make(map[string]json.RawMessage),
		jobs:       make(map[string]*Job),
		stop:       make(chan struct{}),
	}
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.reaper()
	// recover open jobs after process restart
	m.recoverOpenJobs()
	return m
}

func (m *Manager) SetStore(st store.Store) {
	m.mu.Lock()
	m.store = st
	m.mu.Unlock()
	m.recoverOpenJobs()
}

func (m *Manager) Close() {
	close(m.stop)
	close(m.queue)
	m.wg.Wait()
}

func (m *Manager) EnqueueOutline(userID string, payload any) (*Job, error) {
	return m.enqueue(userID, TypeOutline, payload, "", "")
}

func (m *Manager) EnqueueSVG(userID string, payload any) (*Job, error) {
	return m.enqueue(userID, TypeSVG, payload, "", "")
}

// EnqueueSVGChild creates a child retry/AI-edit job under parentJobID.
// Child jobs are hidden from the main task list.
func (m *Manager) EnqueueSVGChild(userID, parentJobID, label string, payload any) (*Job, error) {
	return m.enqueue(userID, TypeSVG, payload, parentJobID, label)
}

func (m *Manager) enqueue(userID string, jobType Type, payload any, parentJobID, label string) (*Job, error) {
	if userID == "" {
		return nil, errors.New("missing user")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	job := &Job{
		ID:          newID(),
		UserID:      userID,
		Type:        jobType,
		Status:      StatusQueued,
		Progress:    0,
		ParentJobID: strings.TrimSpace(parentJobID),
		Label:       strings.TrimSpace(label),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := m.persist(job, raw, nil); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.jobs[job.ID] = cloneJob(job)
	m.payloads[job.ID] = append(json.RawMessage(nil), raw...)
	m.mu.Unlock()

	select {
	case m.queue <- job.ID:
	default:
		job.Status = StatusFailed
		job.Error = "任务队列已满，请稍后重试"
		job.UpdatedAt = time.Now().UTC()
		finished := job.UpdatedAt
		job.FinishedAt = &finished
		_ = m.persist(job, raw, nil)
		m.mu.Lock()
		delete(m.payloads, job.ID)
		m.jobs[job.ID] = cloneJob(job)
		m.mu.Unlock()
		return cloneJob(job), errors.New(job.Error)
	}
	return cloneJob(job), nil
}

func (m *Manager) Get(userID, id string) (*Job, error) {
	// prefer memory then store
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()
	if ok {
		if job.UserID != userID {
			return nil, ErrForbidden
		}
		return cloneJob(job), nil
	}
	rec, err := m.store.GetGenerationJob(context.Background(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rec.UserID != userID {
		return nil, ErrForbidden
	}
	job = recordToJob(rec)
	m.mu.Lock()
	m.jobs[id] = cloneJob(job)
	if len(rec.PayloadJSON) > 0 {
		m.payloads[id] = append(json.RawMessage(nil), rec.PayloadJSON...)
	}
	m.mu.Unlock()
	return cloneJob(job), nil
}

func (m *Manager) ListChildren(parentJobID string, limit int) []*Job {
	if limit <= 0 {
		limit = 50
	}
	if m.store == nil {
		return nil
	}
	recs, err := m.store.ListChildGenerationJobs(context.Background(), parentJobID, limit)
	if err != nil {
		return nil
	}
	out := make([]*Job, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToJob(rec))
	}
	return out
}

func (m *Manager) List(userID string, limit int) []*Job {
	if limit <= 0 {
		limit = 20
	}
	recs, err := m.store.ListGenerationJobsByUser(context.Background(), userID, limit)
	if err != nil {
		// fallback memory
		m.mu.RLock()
		defer m.mu.RUnlock()
		out := make([]*Job, 0, limit)
		for _, job := range m.jobs {
			if job.UserID == userID && strings.TrimSpace(job.ParentJobID) == "" {
				out = append(out, cloneJob(job))
			}
		}
		sortJobsDesc(out)
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	out := make([]*Job, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToJob(rec))
	}
	return out
}

func (m *Manager) Payload(id string) (json.RawMessage, bool) {
	m.mu.RLock()
	raw, ok := m.payloads[id]
	m.mu.RUnlock()
	if ok {
		return append(json.RawMessage(nil), raw...), true
	}
	rec, err := m.store.GetGenerationJob(context.Background(), id)
	if err != nil || len(rec.PayloadJSON) == 0 {
		return nil, false
	}
	m.mu.Lock()
	m.payloads[id] = append(json.RawMessage(nil), rec.PayloadJSON...)
	m.mu.Unlock()
	return append(json.RawMessage(nil), rec.PayloadJSON...), true
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case id, ok := <-m.queue:
			if !ok {
				return
			}
			m.execute(id)
		}
	}
}

func (m *Manager) execute(id string) {
	job, payload, err := m.loadForExecute(id)
	if err != nil || job == nil {
		return
	}
	now := time.Now().UTC()
	job.Status = StatusRunning
	job.Progress = 5
	job.StartedAt = &now
	job.UpdatedAt = now
	_ = m.persist(job, payload, nil)
	m.cache(job, payload)

	ctx := context.Background()
	var (
		result json.RawMessage
		runErr error
	)
	switch job.Type {
	case TypeOutline:
		result, runErr = m.runOutline(ctx, cloneJob(job))
	case TypeSVG:
		result, runErr = m.runSVG(ctx, cloneJob(job))
	default:
		runErr = errors.New("unknown job type")
	}

	finished := time.Now().UTC()
	job.FinishedAt = &finished
	job.UpdatedAt = finished
	job.Progress = 100
	if runErr != nil {
		job.Status = StatusFailed
		job.Error = runErr.Error()
		// Keep partial/detailed result when runner returns one (e.g. per-slide failures).
		if len(result) > 0 {
			job.Result = result
		}
		_ = m.persist(job, payload, result)
		m.cache(job, nil)
		return
	}
	job.Status = StatusSucceeded
	job.Error = ""
	job.Result = result
	_ = m.persist(job, payload, result)
	m.cache(job, nil)
}

func (m *Manager) loadForExecute(id string) (*Job, json.RawMessage, error) {
	m.mu.RLock()
	job := m.jobs[id]
	payload := m.payloads[id]
	m.mu.RUnlock()
	if job != nil && len(payload) > 0 {
		return cloneJob(job), append(json.RawMessage(nil), payload...), nil
	}
	rec, err := m.store.GetGenerationJob(context.Background(), id)
	if err != nil {
		return nil, nil, err
	}
	return recordToJob(rec), append(json.RawMessage(nil), rec.PayloadJSON...), nil
}

func (m *Manager) persist(job *Job, payload, result json.RawMessage) error {
	if m.store == nil {
		return store.ErrInvalidStore
	}
	rec := jobToRecord(job, payload, result)
	return m.store.SaveGenerationJob(context.Background(), rec)
}

func (m *Manager) cache(job *Job, payload json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = cloneJob(job)
	if payload == nil {
		delete(m.payloads, job.ID)
	} else {
		m.payloads[job.ID] = append(json.RawMessage(nil), payload...)
	}
}

func (m *Manager) recoverOpenJobs() {
	if m.store == nil {
		return
	}
	recs, err := m.store.ListOpenGenerationJobs(context.Background(), 500)
	if err != nil {
		return
	}
	for _, rec := range recs {
		// Requeue previously running/queued jobs so they continue after restart or stall.
		if rec.Status == "running" || rec.Status == "queued" {
			rec.Status = "queued"
			rec.Progress = 0
			rec.StartedAt = nil
			rec.FinishedAt = nil
			rec.Error = ""
			rec.UpdatedAt = time.Now().UTC()
			_ = m.store.SaveGenerationJob(context.Background(), rec)
		}
		job := recordToJob(rec)
		m.mu.Lock()
		// avoid double-queueing the same id if already known as queued/running in memory
		if existing, ok := m.jobs[job.ID]; ok {
			if existing.Status == StatusQueued || existing.Status == StatusRunning {
				m.mu.Unlock()
				continue
			}
		}
		m.jobs[job.ID] = cloneJob(job)
		if len(rec.PayloadJSON) > 0 {
			m.payloads[job.ID] = append(json.RawMessage(nil), rec.PayloadJSON...)
		}
		m.mu.Unlock()
		select {
		case m.queue <- job.ID:
		default:
			// queue full; leave persisted queued for later process
		}
	}
}

func (m *Manager) reaper() {
	defer m.wg.Done()
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			now := time.Now().UTC()
			cutoff := now.Add(-m.ttl)
			staleBefore := now.Add(-20 * time.Minute)
			// cleanup memory cache of finished old jobs and reclaim stale running jobs
			m.mu.Lock()
			for id, job := range m.jobs {
				if (job.Status == StatusSucceeded || job.Status == StatusFailed) && job.UpdatedAt.Before(cutoff) {
					delete(m.jobs, id)
					delete(m.payloads, id)
					if m.store != nil {
						_ = m.store.DeleteGenerationJob(context.Background(), id)
					}
					continue
				}
				// A worker may die mid-request; requeue long-running jobs.
				if job.Status == StatusRunning && job.StartedAt != nil && job.StartedAt.Before(staleBefore) {
					job.Status = StatusQueued
					job.Progress = 0
					job.StartedAt = nil
					job.FinishedAt = nil
					job.Error = ""
					job.UpdatedAt = now
					m.jobs[id] = cloneJob(job)
					if m.store != nil {
						_ = m.persist(job, m.payloads[id], nil)
					}
					select {
					case m.queue <- id:
					default:
					}
				}
			}
			m.mu.Unlock()
			// also recover anything only present in store
			m.recoverOpenJobs()
		}
	}
}

func jobToRecord(job *Job, payload, result json.RawMessage) store.GenerationJobRecord {
	rec := store.GenerationJobRecord{
		ID:          job.ID,
		UserID:      job.UserID,
		Type:        string(job.Type),
		Status:      string(job.Status),
		Progress:    job.Progress,
		Error:       job.Error,
		ParentJobID: job.ParentJobID,
		Label:       job.Label,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		StartedAt:   job.StartedAt,
		FinishedAt:  job.FinishedAt,
	}
	if len(payload) > 0 {
		rec.PayloadJSON = append(json.RawMessage(nil), payload...)
	}
	if len(result) > 0 {
		rec.ResultJSON = append(json.RawMessage(nil), result...)
	} else if len(job.Result) > 0 {
		rec.ResultJSON = append(json.RawMessage(nil), job.Result...)
	}
	return rec
}

func recordToJob(rec store.GenerationJobRecord) *Job {
	job := &Job{
		ID:          rec.ID,
		UserID:      rec.UserID,
		Type:        Type(rec.Type),
		Status:      Status(rec.Status),
		Progress:    rec.Progress,
		Error:       rec.Error,
		ParentJobID: rec.ParentJobID,
		Label:       rec.Label,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
		StartedAt:   rec.StartedAt,
		FinishedAt:  rec.FinishedAt,
	}
	if len(rec.ResultJSON) > 0 {
		job.Result = append(json.RawMessage(nil), rec.ResultJSON...)
	}
	return job
}

// PatchOutlineResult updates an outline job's result outline payload.
func (m *Manager) PatchOutlineResult(userID, jobID string, outline any) (*Job, error) {
	if m.store == nil {
		return nil, store.ErrInvalidStore
	}
	rec, err := m.store.GetGenerationJob(context.Background(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rec.UserID != userID {
		return nil, ErrForbidden
	}
	if rec.Type != string(TypeOutline) {
		return nil, errors.New("仅支持更新架构生成任务")
	}
	result := map[string]any{}
	if len(rec.ResultJSON) > 0 {
		_ = json.Unmarshal(rec.ResultJSON, &result)
	}
	// Keep traces/context if present.
	result["outline"] = outline
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rec.ResultJSON = raw
	rec.UpdatedAt = now
	// Edited outline is treated as a successful user-confirmed artifact.
	rec.Status = string(StatusSucceeded)
	rec.Error = ""
	rec.Progress = 100
	if rec.FinishedAt == nil {
		rec.FinishedAt = &now
	}
	if err := m.store.SaveGenerationJob(context.Background(), rec); err != nil {
		return nil, err
	}
	job := recordToJob(rec)
	m.mu.Lock()
	m.jobs[job.ID] = cloneJob(job)
	m.mu.Unlock()
	return cloneJob(job), nil
}

// PatchSVGSlide merges one slide into an existing SVG job result and persists it.
// Used when user regenerates/revises pages after the original job finished.
func (m *Manager) PatchSVGSlide(userID, jobID string, slide map[string]any, outline any) (*Job, error) {
	if m.store == nil {
		return nil, store.ErrInvalidStore
	}
	rec, err := m.store.GetGenerationJob(context.Background(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rec.UserID != userID {
		return nil, ErrForbidden
	}
	if rec.Type != string(TypeSVG) {
		return nil, errors.New("仅支持更新页面生成任务")
	}

	result := map[string]any{}
	if len(rec.ResultJSON) > 0 {
		_ = json.Unmarshal(rec.ResultJSON, &result)
	}
	// slides array
	slidesAny, _ := result["slides"].([]any)
	slides := make([]any, 0, len(slidesAny)+1)
	slides = append(slides, slidesAny...)
	slideID, _ := slide["slideId"].(string)
	replaced := false
	for i, item := range slides {
		mItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if sid, _ := mItem["slideId"].(string); sid == slideID {
			slides[i] = slide
			replaced = true
			break
		}
	}
	if !replaced {
		slides = append(slides, slide)
	}
	result["slides"] = slides
	if outline != nil {
		result["outline"] = outline
	}
	// recompute failed count
	failed := 0
	for _, item := range slides {
		mItem, ok := item.(map[string]any)
		if !ok {
			failed++
			continue
		}
		errText, _ := mItem["error"].(string)
		svgText, _ := mItem["svg"].(string)
		if strings.TrimSpace(errText) != "" || strings.TrimSpace(svgText) == "" {
			failed++
		}
	}
	result["failed"] = failed
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rec.ResultJSON = raw
	rec.UpdatedAt = now
	// If job previously failed/partial and now has successes, keep status succeeded when any page ok.
	// Only mark failed when every slide still failed.
	if failed == 0 && len(slides) > 0 {
		rec.Status = string(StatusSucceeded)
		rec.Error = ""
		rec.Progress = 100
		if rec.FinishedAt == nil {
			rec.FinishedAt = &now
		}
	} else if failed == len(slides) && len(slides) > 0 {
		rec.Status = string(StatusFailed)
		if strings.TrimSpace(rec.Error) == "" {
			rec.Error = "全部页面生成失败"
		}
		rec.Progress = 100
		if rec.FinishedAt == nil {
			rec.FinishedAt = &now
		}
	} else {
		// partial success: treat as succeeded with failed pages in result
		rec.Status = string(StatusSucceeded)
		if failed > 0 {
			rec.Error = ""
		}
		rec.Progress = 100
		if rec.FinishedAt == nil {
			rec.FinishedAt = &now
		}
	}
	if err := m.store.SaveGenerationJob(context.Background(), rec); err != nil {
		return nil, err
	}
	job := recordToJob(rec)
	m.mu.Lock()
	m.jobs[job.ID] = cloneJob(job)
	m.mu.Unlock()
	return cloneJob(job), nil
}

// PatchSVGSlides replaces the full slides array on an SVG job result.
func (m *Manager) PatchSVGSlides(userID, jobID string, slides []any, outline any) (*Job, error) {
	if m.store == nil {
		return nil, store.ErrInvalidStore
	}
	rec, err := m.store.GetGenerationJob(context.Background(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if rec.UserID != userID {
		return nil, ErrForbidden
	}
	if rec.Type != string(TypeSVG) {
		return nil, errors.New("仅支持更新页面生成任务")
	}
	result := map[string]any{}
	if len(rec.ResultJSON) > 0 {
		_ = json.Unmarshal(rec.ResultJSON, &result)
	}
	result["slides"] = slides
	if outline != nil {
		result["outline"] = outline
	}
	failed := 0
	for _, item := range slides {
		mItem, ok := item.(map[string]any)
		if !ok {
			failed++
			continue
		}
		errText, _ := mItem["error"].(string)
		svgText, _ := mItem["svg"].(string)
		if strings.TrimSpace(errText) != "" || strings.TrimSpace(svgText) == "" {
			failed++
		}
	}
	result["failed"] = failed
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rec.ResultJSON = raw
	rec.UpdatedAt = now
	if failed == 0 && len(slides) > 0 {
		rec.Status = string(StatusSucceeded)
		rec.Error = ""
	} else if failed == len(slides) && len(slides) > 0 {
		rec.Status = string(StatusFailed)
		if strings.TrimSpace(rec.Error) == "" {
			rec.Error = "全部页面生成失败"
		}
	} else {
		rec.Status = string(StatusSucceeded)
		rec.Error = ""
	}
	rec.Progress = 100
	if rec.FinishedAt == nil {
		rec.FinishedAt = &now
	}
	if err := m.store.SaveGenerationJob(context.Background(), rec); err != nil {
		return nil, err
	}
	job := recordToJob(rec)
	m.mu.Lock()
	m.jobs[job.ID] = cloneJob(job)
	m.mu.Unlock()
	return cloneJob(job), nil
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	cp := *job
	if job.Result != nil {
		cp.Result = append(json.RawMessage(nil), job.Result...)
	}
	if job.StartedAt != nil {
		t := *job.StartedAt
		cp.StartedAt = &t
	}
	if job.FinishedAt != nil {
		t := *job.FinishedAt
		cp.FinishedAt = &t
	}
	return &cp
}

func sortJobsDesc(jobs []*Job) {
	for i := 0; i < len(jobs); i++ {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[j].CreatedAt.After(jobs[i].CreatedAt) {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
