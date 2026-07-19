package requestid_test

import (
	"context"
	"testing"

	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

func TestRequestAndJobIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = requestid.WithRequestID(ctx, "req-1")
	ctx = requestid.WithJobID(ctx, "job-1")

	if got, ok := requestid.FromContext(ctx); !ok || got != "req-1" {
		t.Fatalf("request id = %q, ok=%v", got, ok)
	}
	if got, ok := requestid.JobIDFromContext(ctx); !ok || got != "job-1" {
		t.Fatalf("job id = %q, ok=%v", got, ok)
	}
}

func TestNewNotEmpty(t *testing.T) {
	if requestid.New() == "" {
		t.Fatal("expected non-empty id")
	}
}
