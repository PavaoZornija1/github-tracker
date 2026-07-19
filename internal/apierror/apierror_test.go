package apierror_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
)

func TestErrorEnvelope(t *testing.T) {
	err := apierror.Conflict("repository already tracked")
	env := err.Envelope()
	if env.Error.Code != apierror.CodeConflict {
		t.Fatalf("code = %q", env.Error.Code)
	}
	if env.Error.Message != "repository already tracked" {
		t.Fatalf("message = %q", env.Error.Message)
	}
	if err.Status != http.StatusConflict {
		t.Fatalf("status = %d", err.Status)
	}
}

func TestAsUnwrap(t *testing.T) {
	root := errors.New("unique violation")
	err := apierror.Wrap(root, http.StatusConflict, apierror.CodeConflict, "duplicate")
	got, ok := apierror.As(err)
	if !ok {
		t.Fatal("expected As to succeed")
	}
	if !errors.Is(got, root) {
		t.Fatal("expected cause to be unwrap-able via errors.Is")
	}
}
