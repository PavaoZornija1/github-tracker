package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestReleaseLockDoesNotDeleteOtherHolder(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	c := NewGitHubCache(rdb, nil, GitHubCacheOptions{
		TTL:     time.Minute,
		LockTTL: time.Second,
	})

	ctx := context.Background()
	owner, name := "golang", "go"
	key := lockKey(owner, name)

	if err := rdb.Set(ctx, key, "holder-b", time.Minute).Err(); err != nil {
		t.Fatalf("set lock: %v", err)
	}
	if err := c.releaseLock(ctx, owner, name, "holder-a"); err != nil {
		t.Fatalf("release stale: %v", err)
	}
	got, err := rdb.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("get lock after stale release: %v", err)
	}
	if got != "holder-b" {
		t.Fatalf("lock value = %q, want holder-b", got)
	}

	if err := c.releaseLock(ctx, owner, name, "holder-b"); err != nil {
		t.Fatalf("release owner: %v", err)
	}
	if n, err := rdb.Exists(ctx, key).Result(); err != nil || n != 0 {
		t.Fatalf("lock should be gone, exists=%d err=%v", n, err)
	}
}
