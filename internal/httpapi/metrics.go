package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/metrics"
)

// MetricsMiddleware increments http_requests_total using Gin route templates.
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		metrics.HTTPRequestsTotal.WithLabelValues(
			c.Request.Method,
			strconv.Itoa(c.Writer.Status()),
			path,
		).Inc()
	}
}
