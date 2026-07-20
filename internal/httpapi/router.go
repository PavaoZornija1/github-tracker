package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/PavaoZornija1/github-tracker/internal/service"
)

// Dependencies for constructing the HTTP router.
type RouterDeps struct {
	Repos   *service.RepoService
	Batches *service.BatchService
	Logger  *slog.Logger
	Ready   ReadyChecker
}

// NewRouter builds the Gin engine with middleware and routes.
func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(RequestID())
	r.Use(AccessLog(deps.Logger))
	r.Use(MetricsMiddleware())
	r.Use(gin.Recovery())

	h := NewRepoHandler(deps.Repos, deps.Logger)
	bh := NewBatchHandler(deps.Batches, deps.Logger)

	api := r.Group("/api")
	{
		api.GET("/repos/stats", h.Stats)
		api.GET("/repos/changes", h.Changes)
		api.POST("/repos/refresh-all", bh.RefreshAll)
		api.GET("/batches/:id", bh.GetBatch)
		api.POST("/batches/:id/enqueue", bh.EnqueueBatch)
		api.GET("/repos", h.List)
		api.POST("/repos", h.Create)
		api.GET("/repos/:id", h.Get)
		api.PATCH("/repos/:id", h.Patch)
		api.DELETE("/repos/:id", h.Delete)
		api.POST("/repos/:id/refresh", h.Refresh)
	}

	r.GET("/", Root)
	r.GET("/healthz", Healthz)
	r.GET("/readyz", Readyz(deps.Ready))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	return r
}
