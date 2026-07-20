package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReadyChecker reports whether the process can accept traffic.
type ReadyChecker func(ctx context.Context) error

// Healthz is a liveness probe: process is up.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz is a readiness probe: required dependencies are reachable.
func Readyz(check ReadyChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if check == nil {
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
			return
		}
		if err := check(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
