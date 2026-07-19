package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

// RepoFetcher fetches a repository from an upstream (typically GitHub).
type RepoFetcher interface {
	GetRepo(ctx context.Context, owner, name string) (*githubclient.Repo, error)
}

// GitHubCache caches GitHub repo payloads in Redis with single-flight locks.
type GitHubCache struct {
	rdb      *redis.Client
	fetcher  RepoFetcher
	ttl      time.Duration
	lockTTL  time.Duration
	waitTick time.Duration
}

// GitHubCacheOptions configures cache TTLs.
type GitHubCacheOptions struct {
	TTL      time.Duration
	LockTTL  time.Duration
	WaitTick time.Duration
}

// NewGitHubCache builds a caching wrapper around a RepoFetcher.
func NewGitHubCache(rdb *redis.Client, fetcher RepoFetcher, opts GitHubCacheOptions) *GitHubCache {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	lockTTL := opts.LockTTL
	if lockTTL <= 0 {
		lockTTL = 30 * time.Second
	}
	tick := opts.WaitTick
	if tick <= 0 {
		tick = 50 * time.Millisecond
	}
	return &GitHubCache{
		rdb:      rdb,
		fetcher:  fetcher,
		ttl:      ttl,
		lockTTL:  lockTTL,
		waitTick: tick,
	}
}

func cacheKey(owner, name string) string {
	return fmt.Sprintf("gh:repo:%s/%s", owner, name)
}

func lockKey(owner, name string) string {
	return fmt.Sprintf("gh:lock:%s/%s", owner, name)
}

// Get returns a cached repo or fetches via single-flight lock so concurrent
// misses across replicas perform exactly one upstream call.
//
// Waiters always check the cache before attempting the lock. That way, when the
// lock holder finishes (writes cache, deletes lock), peers read the cache instead
// of racing to fetch again. If the holder dies, the lock TTL expires and a waiter
// can acquire it on the next loop.
func (c *GitHubCache) Get(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	for {
		if repo, ok, err := c.getCached(ctx, owner, name); err != nil {
			return nil, err
		} else if ok {
			return repo, nil
		}

		acquired, err := c.rdb.SetNX(ctx, lockKey(owner, name), "1", c.lockTTL).Result()
		if err != nil {
			return nil, fmt.Errorf("acquire fetch lock: %w", err)
		}
		if acquired {
			// Another replica may have populated the cache between our miss and lock win.
			if repo, ok, err := c.getCached(ctx, owner, name); err != nil {
				_ = c.rdb.Del(ctx, lockKey(owner, name)).Err()
				return nil, err
			} else if ok {
				_ = c.rdb.Del(ctx, lockKey(owner, name)).Err()
				return repo, nil
			}
			return c.fetchAndCache(ctx, owner, name)
		}

		timer := time.NewTimer(c.waitTick)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Invalidate removes the cached payload for owner/name (used on explicit refresh).
func (c *GitHubCache) Invalidate(ctx context.Context, owner, name string) error {
	return c.rdb.Del(ctx, cacheKey(owner, name)).Err()
}

func (c *GitHubCache) getCached(ctx context.Context, owner, name string) (*githubclient.Repo, bool, error) {
	val, err := c.rdb.Get(ctx, cacheKey(owner, name)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get: %w", err)
	}
	var repo githubclient.Repo
	if err := json.Unmarshal(val, &repo); err != nil {
		return nil, false, fmt.Errorf("decode cache: %w", err)
	}
	return &repo, true, nil
}

func (c *GitHubCache) fetchAndCache(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	defer func() {
		_ = c.rdb.Del(ctx, lockKey(owner, name)).Err()
	}()

	repo, err := c.fetcher.GetRepo(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(repo)
	if err != nil {
		return nil, fmt.Errorf("encode cache: %w", err)
	}
	if err := c.rdb.Set(ctx, cacheKey(owner, name), payload, c.ttl).Err(); err != nil {
		return nil, fmt.Errorf("redis set: %w", err)
	}
	return repo, nil
}
