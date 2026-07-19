package jobs

import (
	"encoding/json"
	"time"
)

type Type string

const (
	TypeOutline Type = "outline"
	TypeSVG     Type = "svg"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Job is a user-owned asynchronous generation task.
type Job struct {
	ID          string          `json:"id"`
	UserID      string          `json:"userId"`
	Type        Type            `json:"type"`
	Status      Status          `json:"status"`
	Progress    int             `json:"progress"`
	Error       string          `json:"error,omitempty"`
	ParentJobID string          `json:"parentJobId,omitempty"`
	Label       string          `json:"label,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	// Children is populated only for detail views.
	Children   []*Job     `json:"children,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type OutlineResult struct {
	Outline any `json:"outline"`
}

type SVGResult struct {
	Slides  any `json:"slides"`
	Outline any `json:"outline,omitempty"`
	Failed  int `json:"failed,omitempty"`
	Quota   any `json:"quota,omitempty"`
}
