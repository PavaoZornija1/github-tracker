package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/PavaoZornija1/github-tracker/internal/platform/logging"
	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

func TestFromContextAddsIDs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx := requestid.WithRequestID(context.Background(), "req-abc")
	ctx = requestid.WithJobID(ctx, "job-xyz")

	logging.FromContext(ctx, base).Info("hello")

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}
	if payload["request_id"] != "req-abc" {
		t.Fatalf("request_id = %v", payload["request_id"])
	}
	if payload["job_id"] != "job-xyz" {
		t.Fatalf("job_id = %v", payload["job_id"])
	}
}
