package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/service"
)

// RepoHandler exposes repository HTTP endpoints.
type RepoHandler struct {
	svc    *service.RepoService
	logger *slog.Logger
}

func NewRepoHandler(svc *service.RepoService, logger *slog.Logger) *RepoHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RepoHandler{svc: svc, logger: logger}
}

type createRepoRequest struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type patchNotesRequest struct {
	Notes string `json:"notes"`
}

func (h *RepoHandler) Create(c *gin.Context) {
	var req createRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteError(c, h.logger, apierror.Validation("invalid JSON body"))
		return
	}
	dto, err := h.svc.Create(c.Request.Context(), req.Owner, req.Name)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusCreated, dto)
}

func (h *RepoHandler) List(c *gin.Context) {
	sort, err := service.ParseListSort(c.Query("sort"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	res, err := h.svc.List(c.Request.Context(), service.ListParams{
		Language: c.Query("language"),
		Sort:     sort,
		Cursor:   c.Query("cursor"),
		Limit:    limit,
	})
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, res)
}

func (h *RepoHandler) Get(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	dto, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, dto)
}

func (h *RepoHandler) Patch(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	var req patchNotesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteError(c, h.logger, apierror.Validation("invalid JSON body"))
		return
	}
	dto, err := h.svc.UpdateNotes(c.Request.Context(), id, req.Notes)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, dto)
}

func (h *RepoHandler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *RepoHandler) Refresh(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	dto, err := h.svc.Refresh(c.Request.Context(), id)
	if err != nil {
		WriteError(c, h.logger, service.MapGitHubError(err))
		return
	}
	WriteJSON(c, http.StatusOK, dto)
}

func (h *RepoHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, stats)
}

func (h *RepoHandler) Changes(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	res, err := h.svc.Changes(c.Request.Context(), service.ChangesParams{
		Since: c.Query("since"),
		Limit: limit,
	})
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, res)
}

func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierror.Validation("invalid repository id")
	}
	return id, nil
}
