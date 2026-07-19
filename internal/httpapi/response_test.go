package httpapi_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/httpapi"
	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

func TestRequestIDMiddlewareGeneratesAndEchoes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpapi.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		id, ok := requestid.FromContext(c.Request.Context())
		if !ok || id == "" {
			t.Fatal("expected request id in context")
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get(requestid.Header) == "" {
		t.Fatal("expected X-Request-ID response header")
	}
}

func TestRequestIDMiddlewareHonorsIncoming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpapi.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(requestid.Header, "incoming-id")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get(requestid.Header); got != "incoming-id" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

func TestWriteErrorUsesEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/err", func(c *gin.Context) {
		httpapi.WriteError(c, slog.Default(), apierror.NotFound("repo not found"))
	})

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
	var body apierror.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Error.Code != apierror.CodeNotFound {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if body.Error.Message != "repo not found" {
		t.Fatalf("message = %q", body.Error.Message)
	}
}
