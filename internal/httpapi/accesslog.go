package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

// AccessLog logs one structured line per request after the handler returns.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		rid, _ := requestid.FromContext(c.Request.Context())
		logger.Info("http_access",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", latency.Milliseconds(),
			"request_id", rid,
		)
	}
}
