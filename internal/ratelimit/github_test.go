package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/ratelimit"
)

func newGate(t *testing.T) (*ratelimit.GitHubGate, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return ratelimit.NewGitHubGate(rdb), mr, rdb
}

func TestAllowDeniedWhenUntilInFuture(t *testing.T) {
	gate, _, rdb := newGate(t)
	ctx := context.Background()
	until := time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
	if err := rdb.Set(ctx, ratelimit.KeyUntil, until, time.Hour).Err(); err != nil {
		t.Fatalf("set until: %v", err)
	}

	err := gate.Allow(ctx)
	ge, ok := githubclient.As(err)
	if !ok || ge.Kind != githubclient.KindRateLimited {
		t.Fatalf("Allow = %v, want KindRateLimited", err)
	}
	if ge.RetryAfter < time.Minute {
		t.Fatalf("RetryAfter = %v, want >= 1m", ge.RetryAfter)
	}
}

func TestAllowWhenRemainingPositive(t *testing.T) {
	gate, _, rdb := newGate(t)
	ctx := context.Background()
	_ = rdb.Set(ctx, ratelimit.KeyRemaining, "10", time.Hour).Err()
	_ = rdb.Set(ctx, ratelimit.KeyReset, "9999999999", time.Hour).Err()
	if err := gate.Allow(ctx); err != nil {
		t.Fatalf("Allow: %v", err)
	}
}

func TestObserveRemainingZeroDeniesUntilReset(t *testing.T) {
	gate, _, _ := newGate(t)
	ctx := context.Background()
	resetAt := time.Now().UTC().Add(3 * time.Minute)

	if err := gate.Observe(ctx, 0, resetAt, 0); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	err := gate.Allow(ctx)
	ge, ok := githubclient.As(err)
	if !ok || ge.Kind != githubclient.KindRateLimited {
		t.Fatalf("Allow after Observe = %v, want KindRateLimited", err)
	}
	if ge.RetryAfter < 2*time.Minute {
		t.Fatalf("RetryAfter = %v, want roughly until reset", ge.RetryAfter)
	}
}

func TestObserveRetryAfterSetsCoolDown(t *testing.T) {
	gate, _, _ := newGate(t)
	ctx := context.Background()
	if err := gate.Observe(ctx, 5, time.Time{}, 90*time.Second); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	err := gate.Allow(ctx)
	if _, ok := githubclient.As(err); !ok {
		t.Fatalf("Allow = %v, want rate limited", err)
	}
}

func TestAllowNilGate(t *testing.T) {
	var gate *ratelimit.GitHubGate
	if err := gate.Allow(context.Background()); err != nil {
		t.Fatalf("nil gate Allow: %v", err)
	}
}

func TestClientObserverUpdatesGate(t *testing.T) {
	gate, _, _ := newGate(t)
	ctx := context.Background()

	resetAt := time.Now().UTC().Add(time.Minute)
	gate.OnRateLimit(ctx, githubclient.RateLimitInfo{
		Remaining:  0,
		ResetAt:    resetAt,
		RetryAfter: time.Minute,
	})
	if err := gate.Allow(ctx); err == nil {
		t.Fatal("expected deny after observer update")
	}
}
