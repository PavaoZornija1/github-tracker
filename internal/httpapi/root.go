package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Root returns a small service index so GET / is not a bare 404.
func Root(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":     "github-tracker",
		"description": "REST API that tracks GitHub repositories (sync fetch, Redis-cached GitHub reads, async refresh-all via RabbitMQ).",
		"docs":        "/swagger/index.html",
		"health":      "/healthz",
		"ready":       "/readyz",
		"metrics":     "/metrics",
		"api_base":    "/api",
		"endpoints": []string{
			"POST   /api/repos",
			"GET    /api/repos",
			"GET    /api/repos/:id",
			"PATCH  /api/repos/:id",
			"DELETE /api/repos/:id",
			"POST   /api/repos/:id/refresh",
			"POST   /api/repos/refresh-all",
			"GET    /api/batches/:id",
			"POST   /api/batches/:id/enqueue",
			"GET    /api/repos/stats",
			"GET    /api/repos/changes",
		},
	})
}
