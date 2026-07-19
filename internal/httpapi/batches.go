package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/service"
)

// BatchHandler exposes async refresh batch endpoints.
type BatchHandler struct {
	svc    *service.BatchService
	logger *slog.Logger
}

func NewBatchHandler(svc *service.BatchService, logger *slog.Logger) *BatchHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BatchHandler{svc: svc, logger: logger}
}

func (h *BatchHandler) RefreshAll(c *gin.Context) {
	res, err := h.svc.StartRefreshAll(c.Request.Context())
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusAccepted, res)
}

func (h *BatchHandler) GetBatch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, apierror.Validation("invalid batch id"))
		return
	}
	status, err := h.svc.GetBatchStatus(c.Request.Context(), id)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, status)
}
