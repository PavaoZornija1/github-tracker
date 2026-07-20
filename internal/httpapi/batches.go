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

// RefreshAll enqueues a refresh job for every tracked repository.
// @Summary Refresh all repositories
// @Description Create a batch and enqueue one async refresh job per repo. Returns 202 immediately.
// @Tags batches
// @Produce json
// @Success 202 {object} service.RefreshAllAccepted
// @Failure 500 {object} apierror.Envelope
// @Router /repos/refresh-all [post]
func (h *BatchHandler) RefreshAll(c *gin.Context) {
	res, err := h.svc.StartRefreshAll(c.Request.Context())
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusAccepted, res)
}

// GetBatch returns progress for an async refresh batch.
// @Summary Get batch status
// @Tags batches
// @Produce json
// @Param id path string true "Batch ID" format(uuid)
// @Success 200 {object} service.BatchStatus
// @Failure 400 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /batches/{id} [get]
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

// EnqueueBatch re-publishes a batch kick so pending jobs can be fanned out again.
// @Summary Re-enqueue batch kick
// @Description Repair path when kick publish failed after job rows were created.
// @Tags batches
// @Produce json
// @Param id path string true "Batch ID" format(uuid)
// @Success 202 {object} map[string]string
// @Failure 400 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 503 {object} apierror.Envelope
// @Router /batches/{id}/enqueue [post]
func (h *BatchHandler) EnqueueBatch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, apierror.Validation("invalid batch id"))
		return
	}
	if err := h.svc.EnqueueBatch(c.Request.Context(), id); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusAccepted, gin.H{"batch_id": id.String()})
}
