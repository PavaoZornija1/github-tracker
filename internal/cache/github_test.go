package cache_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/PavaoZornija1/github-tracker/internal/cache"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

type countingFetcher struct {
	calls atomic.Int64
	repo  *githubclient.Repo
	err   error
	delay time.Duration
}

func (f *countingFetcher) GetRepo(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.repo, nil
}

func newTestCache(t *testing.T, fetcher cache.RepoFetcher) (*cache.GitHubCache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	c := cache.NewGitHubCache(rdb, fetcher, cache.GitHubCacheOptions{
		TTL:      time.Minute,
		LockTTL:  200 * time.Millisecond,
		WaitTick: 10 * time.Millisecond,
	})
	return c, mr
}

func TestSingleFlightExactlyOneUpstreamCall(t *testing.T) {
	fetcher := &countingFetcher{
		repo: &githubclient.Repo{
			Owner:    "golang",
			Name:     "go",
			FullName: "golang/go",
			Stars:    1,
			HTMLURL:  "https://github.com/golang/go",
		},
		delay: 80 * time.Millisecond,
	}
	c, _ := newTestCache(t, fetcher)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := c.Get(context.Background(), "golang", "go")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if got := fetcher.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want exactly 1", got)
	}
}

func TestInvalidateForcesRefetch(t *testing.T) {
	fetcher := &countingFetcher{
		repo: &githubclient.Repo{
			Owner:    "golang",
			Name:     "go",
			FullName: "golang/go",
			HTMLURL:  "https://github.com/golang/go",
		},
	}
	c, _ := newTestCache(t, fetcher)

	if _, err := c.Get(context.Background(), "golang", "go"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.Get(context.Background(), "golang", "go"); err != nil {
		t.Fatalf("cached Get: %v", err)
	}
	if fetcher.calls.Load() != 1 {
		t.Fatalf("calls before invalidate = %d", fetcher.calls.Load())
	}
	if err := c.Invalidate(context.Background(), "golang", "go"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if _, err := c.Get(context.Background(), "golang", "go"); err != nil {
		t.Fatalf("Get after invalidate: %v", err)
	}
	if fetcher.calls.Load() != 2 {
		t.Fatalf("calls after invalidate = %d, want 2", fetcher.calls.Load())
	}
}

func TestLockHolderDeathAllowsRetry(t *testing.T) {
	fetcher := &countingFetcher{
		repo: &githubclient.Repo{
			Owner:    "golang",
			Name:     "go",
			FullName: "golang/go",
			HTMLURL:  "https://github.com/golang/go",
		},
	}
	c, mr := newTestCache(t, fetcher)

	// Simulate another replica holding the lock then dying before writing cache.
	// miniredis TTLs advance via FastForward, not wall clock.
	if err := mr.Set("gh:lock:golang/go", "1"); err != nil {
		t.Fatalf("set lock: %v", err)
	}
	mr.SetTTL("gh:lock:golang/go", 30*time.Millisecond)

	done := make(chan struct {
		repo *githubclient.Repo
		err  error
	}, 1)
	go func() {
		repo, err := c.Get(context.Background(), "golang", "go")
		done <- struct {
			repo *githubclient.Repo
			err  error
		}{repo, err}
	}()

	time.Sleep(20 * time.Millisecond)
	mr.FastForward(50 * time.Millisecond)

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Get after lock expiry: %v", res.err)
		}
		if res.repo.FullName != "golang/go" {
			t.Fatalf("repo = %+v", res.repo)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Get after lock expiry")
	}
	if fetcher.calls.Load() != 1 {
		t.Fatalf("calls = %d", fetcher.calls.Load())
	}
}
