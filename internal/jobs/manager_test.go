package jobs

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"wty5.cn/ppt-gen/internal/store"
)

func testStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenConfiguredStore(context.Background(), store.Config{Kind: store.StorageJSON, Path: filepath.Join(dir, "app.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestManagerOutlineJobLifecycle(t *testing.T) {
	st := testStore(t)
	m := NewManager(Options{Workers: 1, TTL: time.Minute, Store: st}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
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
			// persisted
			rec, err := st.GetGenerationJob(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Status != "succeeded" {
				t.Fatalf("persisted status=%s", rec.Status)
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
	st := testStore(t)
	m := NewManager(Options{Workers: 1, Store: st}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, nil)
	defer m.Close()
	job, err := m.EnqueueOutline("user-a", map[string]string{"topic": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("user-b", job.ID); err != ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestManagerResumeFromStore(t *testing.T) {
	st := testStore(t)
	// first manager creates and finishes
	m1 := NewManager(Options{Workers: 1, Store: st}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"outline": map[string]string{"title": "persisted"}})
	}, nil)
	job, err := m1.EnqueueOutline("user-1", map[string]string{"topic": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := m1.Get("user-1", job.ID)
		if got != nil && got.Status == StatusSucceeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m1.Close()

	// new manager should still load job from store
	m2 := NewManager(Options{Workers: 1, Store: st}, func(ctx context.Context, job *Job) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, nil)
	defer m2.Close()
	got, err := m2.Get("user-1", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("status=%s", got.Status)
	}
}
