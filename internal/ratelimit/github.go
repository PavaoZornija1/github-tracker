package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

// Redis keys shared across API and worker replicas (not a job queue).
const (
	KeyUntil     = "github:rate_limit_until"
	KeyRemaining = "github:rate_limit_remaining"
	KeyReset     = "github:rate_limit_reset"
)

// GitHubGate is a fleet-wide pre-flight gate for GitHub HTTP calls.
type GitHubGate struct {
	rdb *redis.Client
	now func() time.Time
}

// NewGitHubGate builds a gate backed by Redis. rdb must be non-nil.
func NewGitHubGate(rdb *redis.Client) *GitHubGate {
	return &GitHubGate{
		rdb: rdb,
		now: time.Now,
	}
}

// Allow returns nil when an upstream GitHub call may proceed.
// When denied, it returns a typed githubclient rate-limit error with RetryAfter set.
func (g *GitHubGate) Allow(ctx context.Context) error {
	if g == nil || g.rdb == nil {
		return nil
	}
	now := g.now().UTC()

	untilRaw, err := g.rdb.Get(ctx, KeyUntil).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("rate gate until: %w", err)
	}
	if err == nil {
		until, parseErr := time.Parse(time.RFC3339Nano, untilRaw)
		if parseErr == nil && until.After(now) {
			return githubclient.NewRateLimited("github rate limit cool-down", until.Sub(now))
		}
	}

	remRaw, err := g.rdb.Get(ctx, KeyRemaining).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("rate gate remaining: %w", err)
	}
	if err == redis.Nil {
		return nil
	}
	remaining, convErr := strconv.Atoi(remRaw)
	if convErr != nil {
		return nil
	}
	if remaining > 0 {
		return nil
	}

	resetRaw, err := g.rdb.Get(ctx, KeyReset).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("rate gate reset: %w", err)
	}
	if err == redis.Nil {
		return nil
	}
	resetUnix, convErr := strconv.ParseInt(resetRaw, 10, 64)
	if convErr != nil {
		return nil
	}
	resetAt := time.Unix(resetUnix, 0).UTC()
	if resetAt.After(now) {
		return githubclient.NewRateLimited("github rate limit exhausted", resetAt.Sub(now))
	}
	return nil
}

// Observe records rate-limit headers from a GitHub response for the whole fleet.
// remaining < 0 means unknown; zero resetAt means unknown.
func (g *GitHubGate) Observe(ctx context.Context, remaining int, resetAt time.Time, retryAfter time.Duration) error {
	if g == nil || g.rdb == nil {
		return nil
	}
	now := g.now().UTC()

	ttl := time.Hour
	if !resetAt.IsZero() && resetAt.After(now) {
		if d := resetAt.Sub(now) + time.Minute; d > ttl {
			ttl = d
		}
	}
	if retryAfter > ttl {
		ttl = retryAfter + time.Minute
	}

	pipe := g.rdb.Pipeline()
	if retryAfter > 0 {
		until := now.Add(retryAfter)
		pipe.Set(ctx, KeyUntil, until.Format(time.RFC3339Nano), ttl)
	} else if remaining == 0 && !resetAt.IsZero() && resetAt.After(now) {
		pipe.Set(ctx, KeyUntil, resetAt.Format(time.RFC3339Nano), ttl)
	}

	if remaining >= 0 {
		pipe.Set(ctx, KeyRemaining, strconv.Itoa(remaining), ttl)
	}
	if !resetAt.IsZero() {
		pipe.Set(ctx, KeyReset, strconv.FormatInt(resetAt.Unix(), 10), ttl)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("rate gate observe: %w", err)
	}
	return nil
}

// OnRateLimit implements githubclient.RateLimitObserver.
func (g *GitHubGate) OnRateLimit(ctx context.Context, info githubclient.RateLimitInfo) {
	_ = g.Observe(ctx, info.Remaining, info.ResetAt, info.RetryAfter)
}
