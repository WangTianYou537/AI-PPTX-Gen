package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestManagerOutlineJobLifecycle(t *testing.T) {
	m := NewManager(Options{Workers: 1, TTL: time.Minute}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"outline": map[string]string{"title": "ok"}})
	}, nil)
	defer m.Close()

	job, err := m.EnqueueOutline("user-1", map[string]string{"topic": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusQueued {
		t.Fatalf("expected queued, got %s", job.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := m.Get("user-1", job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == StatusSucceeded {
			if len(got.Result) == 0 {
				t.Fatal("missing result")
			}
			return
		}
		if got.Status == StatusFailed {
			t.Fatalf("job failed: %s", got.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
}

func TestManagerForbidsOtherUser(t *testing.T) {
	m := NewManager(Options{Workers: 1}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, nil)
	defer m.Close()
	job, err := m.EnqueueOutline("user-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("user-b", job.ID); err != ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
