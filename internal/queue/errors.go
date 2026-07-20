package queue

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// TransientError signals a retryable processing failure.
type TransientError struct {
	Err            error
	RetryAfter     time.Duration
	CountAsAttempt bool // when false (rate-limit), republish keeps the same attempt
}

func (e *TransientError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "transient error"
}

func (e *TransientError) Unwrap() error { return e.Err }

// NewTransient wraps err as retryable, optionally honoring Retry-After.
// Counts toward the worker retry budget by default.
func NewTransient(err error, retryAfter time.Duration) error {
	return &TransientError{Err: err, RetryAfter: retryAfter, CountAsAttempt: true}
}

// NewRateLimited wraps a rate-limit / cool-down wait that must not burn retries.
func NewRateLimited(err error, retryAfter time.Duration) error {
	return &TransientError{Err: err, RetryAfter: retryAfter, CountAsAttempt: false}
}

// AsTransient extracts *TransientError from err's chain.
func AsTransient(err error, target **TransientError) bool {
	return errors.As(err, target)
}

// DefaultBackoff is exponential backoff with jitter, floored by Retry-After.
func DefaultBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(base/2) + 1))
	d := base/2 + jitter
	if retryAfter > d {
		return retryAfter
	}
	return d
}

// Permanent wraps a non-retryable failure reason.
func Permanent(reason string) error {
	return fmt.Errorf("%s", reason)
}
