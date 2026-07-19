package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

// NewJSON returns a JSON slog handler writing to stdout at the given level.
func NewJSON(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// FromContext returns a logger with request_id and/or job_id attributes when present.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	attrs := make([]any, 0, 4)
	if id, ok := requestid.FromContext(ctx); ok {
		attrs = append(attrs, "request_id", id)
	}
	if id, ok := requestid.JobIDFromContext(ctx); ok {
		attrs = append(attrs, "job_id", id)
	}
	if len(attrs) == 0 {
		return base
	}
	return base.With(attrs...)
}
