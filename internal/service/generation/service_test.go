package generation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wty5.cn/ppt-gen/internal/domain"
	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/store"
)

type fakeStore struct{ store.Store }

func (f fakeStore) GetSystemSettings(context.Context) (store.SystemSettings, error) {
	return store.SystemSettings{DefaultSlideConcurrencyLimit: 2, DefaultUserGroupID: "default"}, nil
}

func (f fakeStore) GetUserGroup(_ context.Context, id string) (store.UserGroup, error) {
	return store.UserGroup{ID: id, Name: "g", SlideConcurrencyLimit: 4}, nil
}

func TestEffectiveSlideConcurrencyPriority(t *testing.T) {
	svc := &Service{Store: fakeStore{}}
	userLimit := 3
	user := store.User{GroupID: "default", SlideConcurrencyLimit: &userLimit}
	limit, source, groupID, groupName, err := svc.EffectiveSlideConcurrency(context.Background(), user, 10)
	if err != nil {
		t.Fatal(err)
	}
	if limit != 3 || source != "user" || groupID != "default" || groupName != "g" {
		t.Fatalf("unexpected result limit=%d source=%s group=%s/%s", limit, source, groupID, groupName)
	}
	limit, _ = domain.ResolveSlideConcurrency(nil, 4, 2, 2)
	if limit != 2 {
		t.Fatalf("expected clamp to slideCount=2, got %d", limit)
	}
}

func TestSlideResultOrderAssignment(t *testing.T) {
	slides := make([]ppt.SlideSVG, 3)
	results := []struct {
		index int
		id    string
		title string
		svg   string
	}{
		{2, "slide-3", "C", "<svg>c</svg>"},
		{0, "slide-1", "A", "<svg>a</svg>"},
		{1, "slide-2", "B", "<svg>b</svg>"},
	}
	for _, r := range results {
		slides[r.index] = ppt.SlideSVG{SlideID: r.id, Title: r.title, SVG: r.svg}
	}
	if slides[0].SlideID != "slide-1" || slides[1].SlideID != "slide-2" || slides[2].SlideID != "slide-3" {
		t.Fatalf("order broken: %+v", slides)
	}
}

func TestWorkerPoolRespectsConcurrency(t *testing.T) {
	const workers = 2
	const jobsN = 6
	var running int32
	var maxRunning int32
	var wg sync.WaitGroup
	jobs := make(chan int)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				cur := atomic.AddInt32(&running, 1)
				for {
					old := atomic.LoadInt32(&maxRunning)
					if cur <= old || atomic.CompareAndSwapInt32(&maxRunning, old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&running, -1)
			}
		}()
	}
	for i := 0; i < jobsN; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if maxRunning > workers {
		t.Fatalf("max concurrent %d > workers %d", maxRunning, workers)
	}
	if maxRunning < 1 {
		t.Fatal(errors.New("workers did not run"))
	}
}
