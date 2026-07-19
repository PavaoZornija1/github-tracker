package githubclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

func TestGetRepoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/golang/go" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":             "go",
			"full_name":        "golang/go",
			"description":      "The Go programming language",
			"stargazers_count": 120000,
			"language":         "Go",
			"html_url":         "https://github.com/golang/go",
			"owner":            map[string]string{"login": "golang"},
		})
	}))
	defer srv.Close()

	client := githubclient.New(githubclient.Options{BaseURL: srv.URL, Timeout: time.Second})
	repo, err := client.GetRepo(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if repo.FullName != "golang/go" || repo.Stars != 120000 {
		t.Fatalf("unexpected repo: %+v", repo)
	}
}

func TestGetRepoErrorClasses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		kind   githubclient.Kind
	}{
		{name: "not_found", status: http.StatusNotFound, kind: githubclient.KindNotFound},
		{name: "unauthorized", status: http.StatusUnauthorized, kind: githubclient.KindUnauthorized},
		{name: "rate_limited", status: http.StatusTooManyRequests, header: http.Header{"Retry-After": []string{"2"}}, kind: githubclient.KindRateLimited},
		{name: "server", status: http.StatusBadGateway, kind: githubclient.KindServer},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, vals := range tc.header {
					for _, v := range vals {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"fail"}`))
			}))
			defer srv.Close()

			client := githubclient.New(githubclient.Options{BaseURL: srv.URL, Timeout: time.Second})
			_, err := client.GetRepo(context.Background(), "o", "n")
			ge, ok := githubclient.As(err)
			if !ok {
				t.Fatalf("expected githubclient.Error, got %v", err)
			}
			if ge.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", ge.Kind, tc.kind)
			}
			if tc.kind == githubclient.KindRateLimited && ge.RetryAfter < time.Second {
				t.Fatalf("RetryAfter = %v", ge.RetryAfter)
			}
		})
	}
}

func TestGetRepoNetwork(t *testing.T) {
	client := githubclient.New(githubclient.Options{
		BaseURL: "http://127.0.0.1:1",
		Timeout: 50 * time.Millisecond,
	})
	_, err := client.GetRepo(context.Background(), "o", "n")
	ge, ok := githubclient.As(err)
	if !ok {
		t.Fatalf("expected githubclient.Error, got %v", err)
	}
	if ge.Kind != githubclient.KindNetwork {
		t.Fatalf("kind = %v, want network", ge.Kind)
	}
	if !ge.Temporary() {
		t.Fatal("network errors should be temporary")
	}
}
