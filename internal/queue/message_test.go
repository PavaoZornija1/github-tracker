package queue_test

import (
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/queue"
	"github.com/google/uuid"
)

func TestRefreshJobRoundTrip(t *testing.T) {
	job := queue.RefreshJob{
		JobID:   uuid.New(),
		BatchID: uuid.New(),
		RepoID:  uuid.New(),
	}
	b, err := job.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := queue.UnmarshalRefreshJob(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != job {
		t.Fatalf("got %+v want %+v", got, job)
	}
}

func TestDefaultBackoffHonorsRetryAfter(t *testing.T) {
	d := queue.DefaultBackoff(1, 5*time.Second)
	if d < 5*time.Second {
		t.Fatalf("backoff = %v, want >= Retry-After", d)
	}
}

func TestClampRetryExpirationAllowsLongRateLimitDelay(t *testing.T) {
	got := queue.ClampRetryExpiration(5*time.Minute, 15*time.Minute)
	if got != 5*time.Minute {
		t.Fatalf("got %v, want 5m (old 60s cap would truncate)", got)
	}
	got = queue.ClampRetryExpiration(20*time.Minute, 15*time.Minute)
	if got != 15*time.Minute {
		t.Fatalf("got %v, want max 15m", got)
	}
	got = queue.ClampRetryExpiration(time.Minute, 0)
	if got != time.Minute {
		t.Fatalf("default max: got %v, want 1m", got)
	}
}

func TestBatchKickRoundTrip(t *testing.T) {
	kick := queue.BatchKick{BatchID: uuid.New()}
	b, err := kick.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := queue.UnmarshalBatchKick(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != kick {
		t.Fatalf("got %+v want %+v", got, kick)
	}
}

