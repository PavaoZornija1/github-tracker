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

func TestTransientError(t *testing.T) {
	err := queue.NewTransient(queue.Permanent("rate limited"), time.Second)
	var te *queue.TransientError
	if !queue.AsTransient(err, &te) {
		t.Fatal("expected transient")
	}
	if te.RetryAfter != time.Second {
		t.Fatalf("RetryAfter = %v", te.RetryAfter)
	}
}
