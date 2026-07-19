package apierror

import (
	"errors"
	"fmt"
	"net/http"
)

// Stable machine-readable error codes returned in the JSON envelope.
const (
	CodeValidation      = "validation_error"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeUnauthorized    = "unauthorized"
	CodeRateLimited     = "rate_limited"
	CodeBadGateway      = "bad_gateway"
	CodeUnavailable     = "service_unavailable"
	CodeInternal        = "internal_error"
)

// Detail is the inner object of the standard API error envelope.
type Detail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Envelope is the consistent JSON error shape for all HTTP responses.
type Envelope struct {
	Error Detail `json:"error"`
}

// Error is an application error with an HTTP status and stable code.
type Error struct {
	Code    string
	Message string
	Status  int
	err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// Envelope returns the JSON body for this error.
func (e *Error) Envelope() Envelope {
	return Envelope{Error: Detail{Code: e.Code, Message: e.Message}}
}

// New builds an Error with the given status, code, and message.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Wrap attaches a cause while preserving status/code/message.
func Wrap(err error, status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, err: err}
}

func Validation(message string) *Error {
	return New(http.StatusBadRequest, CodeValidation, message)
}

func NotFound(message string) *Error {
	return New(http.StatusNotFound, CodeNotFound, message)
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, CodeConflict, message)
}

func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, CodeUnauthorized, message)
}

func RateLimited(message string) *Error {
	return New(http.StatusTooManyRequests, CodeRateLimited, message)
}

func BadGateway(message string) *Error {
	return New(http.StatusBadGateway, CodeBadGateway, message)
}

func Unavailable(message string) *Error {
	return New(http.StatusServiceUnavailable, CodeUnavailable, message)
}

func Internal(message string) *Error {
	return New(http.StatusInternalServerError, CodeInternal, message)
}

// As extracts *Error from err's chain.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
