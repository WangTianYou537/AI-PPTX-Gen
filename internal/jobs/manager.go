package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("job not found")
	ErrForbidden = errors.New("job access denied")
)

// Manager keeps process-local async jobs and runs them with bounded workers.
type Manager struct {
	mu      sync.RWMutex
	jobs    map[string]*Job
	queue   chan string
	workers int
	ttl     time.Duration

	runOutline func(ctx context.Context, job *Job) (json.RawMessage, error)
	runSVG     func(ctx context.Context, job *Job) (json.RawMessage, error)

	// payloads keep input outside the public Job JSON
	payloads map[string]any
	stop     chan struct{}
	wg       sync.WaitGroup
}

type Options struct {
	Workers int
	TTL     time.Duration
}

func NewManager(opts Options, runOutline func(context.Context, *Job) (json.RawMessage, error), runSVG func(context.Context, *Job) (json.RawMessage, error)) *Manager {
	if opts.Workers < 1 {
		opts.Workers = 2
	}
	if opts.TTL <= 0 {
		opts.TTL = 2 * time.Hour
	}
	m := &Manager{
		jobs:       make(map[string]*Job),
		queue:      make(chan string, 256),
		workers:    opts.Workers,
		ttl:        opts.TTL,
		runOutline: runOutline,
		runSVG:     runSVG,
		payloads:   make(map[string]any),
		stop:       make(chan struct{}),
	}
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.reaper()
	return m
}

func (m *Manager) Close() {
	close(m.stop)
	// drain workers by closing queue after stop signal handled carefully:
	// workers select on stop and queue; close queue to unblock.
	close(m.queue)
	m.wg.Wait()
}

func (m *Manager) EnqueueOutline(userID string, payload any) (*Job, error) {
	return m.enqueue(userID, TypeOutline, payload)
}

func (m *Manager) EnqueueSVG(userID string, payload any) (*Job, error) {
	return m.enqueue(userID, TypeSVG, payload)
}

func (m *Manager) enqueue(userID string, jobType Type, payload any) (*Job, error) {
	if userID == "" {
		return nil, errors.New("missing user")
	}
	now := time.Now().UTC()
	job := &Job{
		ID:        newID(),
		UserID:    userID,
		Type:      jobType,
		Status:    StatusQueued,
		Progress:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.payloads[job.ID] = payload
	m.mu.Unlock()

	select {
	case m.queue <- job.ID:
	default:
		m.mu.Lock()
		job.Status = StatusFailed
		job.Error = "任务队列已满，请稍后重试"
		job.UpdatedAt = time.Now().UTC()
		finished := job.UpdatedAt
		job.FinishedAt = &finished
		delete(m.payloads, job.ID)
		m.mu.Unlock()
		return cloneJob(job), errors.New(job.Error)
	}
	return cloneJob(job), nil
}

func (m *Manager) Get(userID, id string) (*Job, error) {
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if job.UserID != userID {
		return nil, ErrForbidden
	}
	return cloneJob(job), nil
}

func (m *Manager) List(userID string, limit int) []*Job {
	if limit <= 0 {
		limit = 20
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Job, 0, limit)
	// reverse-ish by created time: scan all and insert
	for _, job := range m.jobs {
		if job.UserID != userID {
			continue
		}
		out = append(out, cloneJob(job))
	}
	// simple sort by CreatedAt desc
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Manager) Payload(id string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.payloads[id]
	return p, ok
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
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	job.Status = StatusRunning
	job.Progress = 5
	job.StartedAt = &now
	job.UpdatedAt = now
	jobType := job.Type
	m.mu.Unlock()

	ctx := context.Background()
	var (
		result json.RawMessage
		err    error
	)
	switch jobType {
	case TypeOutline:
		result, err = m.runOutline(ctx, cloneJob(job))
	case TypeSVG:
		result, err = m.runSVG(ctx, cloneJob(job))
	default:
		err = errors.New("unknown job type")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok = m.jobs[id]
	if !ok {
		return
	}
	finished := time.Now().UTC()
	job.FinishedAt = &finished
	job.UpdatedAt = finished
	delete(m.payloads, id)
	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
		job.Progress = 100
		return
	}
	job.Status = StatusSucceeded
	job.Result = result
	job.Progress = 100
	job.Error = ""
}

func (m *Manager) reaper() {
	defer m.wg.Done()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().UTC().Add(-m.ttl)
			m.mu.Lock()
			for id, job := range m.jobs {
				if job.Status == StatusSucceeded || job.Status == StatusFailed {
					if job.UpdatedAt.Before(cutoff) {
						delete(m.jobs, id)
						delete(m.payloads, id)
					}
				}
			}
			m.mu.Unlock()
		}
	}
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

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
