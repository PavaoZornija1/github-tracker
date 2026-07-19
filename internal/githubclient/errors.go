package githubclient

import (
	"errors"
	"fmt"
	"time"
)

// Kind classifies GitHub / transport failures for retry and HTTP mapping.
type Kind int

const (
	KindUnknown Kind = iota
	KindNotFound
	KindUnauthorized
	KindRateLimited
	KindServer
	KindNetwork
)

func (k Kind) String() string {
	switch k {
	case KindNotFound:
		return "not_found"
	case KindUnauthorized:
		return "unauthorized"
	case KindRateLimited:
		return "rate_limited"
	case KindServer:
		return "server_error"
	case KindNetwork:
		return "network"
	default:
		return "unknown"
	}
}

// Error is a typed GitHub client failure.
type Error struct {
	Kind       Kind
	StatusCode int
	Message    string
	RetryAfter time.Duration
	Remaining  int
	err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return fmt.Sprintf("github %s: %s: %v", e.Kind, e.Message, e.err)
	}
	return fmt.Sprintf("github %s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.err }

func (e *Error) Temporary() bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case KindRateLimited, KindServer, KindNetwork:
		return true
	default:
		return false
	}
}

// As extracts *Error from err's chain.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func notFound(msg string) *Error {
	return &Error{Kind: KindNotFound, StatusCode: 404, Message: msg}
}

func unauthorized(msg string) *Error {
	return &Error{Kind: KindUnauthorized, StatusCode: 401, Message: msg}
}

func rateLimited(msg string, retryAfter time.Duration, remaining int) *Error {
	return &Error{
		Kind:       KindRateLimited,
		StatusCode: 429,
		Message:    msg,
		RetryAfter: retryAfter,
		Remaining:  remaining,
	}
}

func serverError(status int, msg string) *Error {
	return &Error{Kind: KindServer, StatusCode: status, Message: msg}
}

func networkError(err error) *Error {
	return &Error{Kind: KindNetwork, Message: "request failed", err: err}
}
