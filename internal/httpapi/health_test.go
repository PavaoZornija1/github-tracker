package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/httpapi"
)

func TestRootIndex(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/", httpapi.Root)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"service":"github-tracker"`) {
		t.Fatalf("body = %s, want service index", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `/swagger/index.html`) {
		t.Fatalf("body = %s, want docs link", w.Body.String())
	}
}

func TestHealthzAlwaysOK(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/healthz", httpapi.Healthz)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestReadyzUnavailableWhenCheckFails(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/readyz", httpapi.Readyz(func(ctx context.Context) error {
		return errors.New("db down")
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestReadyzOKWhenCheckPasses(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/readyz", httpapi.Readyz(func(ctx context.Context) error { return nil }))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}
