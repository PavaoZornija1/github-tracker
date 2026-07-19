package requestid

import (
	"context"

	"github.com/google/uuid"
)

type contextKey int

const (
	requestIDKey contextKey = iota + 1
	jobIDKey
)

// Header is the HTTP header used to propagate request IDs.
const Header = "X-Request-ID"

// New generates a new opaque request/job identifier.
func New() string {
	return uuid.NewString()
}

// WithRequestID returns a child context carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// FromContext returns the request ID if present.
func FromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok && id != ""
}

// WithJobID returns a child context carrying the worker job ID.
func WithJobID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, jobIDKey, id)
}

// JobIDFromContext returns the job ID if present.
func JobIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(jobIDKey).(string)
	return id, ok && id != ""
}
