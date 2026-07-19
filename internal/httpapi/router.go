package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/service"
)

// Dependencies for constructing the HTTP router.
type RouterDeps struct {
	Repos    *service.RepoService
	Logger *slog.Logger
}

// NewRouter builds the Gin engine with middleware and routes.
func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(RequestID())

	h := NewRepoHandler(deps.Repos, deps.Logger)

	api := r.Group("/api")
	{
		api.GET("/repos/stats", h.Stats)
		api.GET("/repos/changes", h.Changes)
		api.GET("/repos", h.List)
		api.POST("/repos", h.Create)
		api.GET("/repos/:id", h.Get)
		api.PATCH("/repos/:id", h.Patch)
		api.DELETE("/repos/:id", h.Delete)
		api.POST("/repos/:id/refresh", h.Refresh)
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return r
}
