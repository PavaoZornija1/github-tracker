package service_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/service"
)

func TestMapGitHubError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "not_found",
			err:    &githubclient.Error{Kind: githubclient.KindNotFound, Message: "missing"},
			status: http.StatusNotFound,
			code:   apierror.CodeNotFound,
		},
		{
			name:   "rate_limited",
			err:    &githubclient.Error{Kind: githubclient.KindRateLimited, Message: "slow down"},
			status: http.StatusTooManyRequests,
			code:   apierror.CodeRateLimited,
		},
		{
			name:   "network",
			err:    &githubclient.Error{Kind: githubclient.KindNetwork, Message: "boom"},
			status: http.StatusServiceUnavailable,
			code:   apierror.CodeUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapped := service.MapGitHubError(tc.err)
			ae, ok := apierror.As(mapped)
			if !ok {
				t.Fatalf("expected apierror, got %v", mapped)
			}
			if ae.Status != tc.status || ae.Code != tc.code {
				t.Fatalf("got status=%d code=%s", ae.Status, ae.Code)
			}
		})
	}

	passthrough := errors.New("other")
	if service.MapGitHubError(passthrough) != passthrough {
		t.Fatal("non-github errors should pass through")
	}
}
