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

// CreateRepoRequest is the body for POST /api/repos.
type CreateRepoRequest struct {
	Owner string `json:"owner" example:"golang"`
	Name  string `json:"name" example:"go"`
}

// PatchNotesRequest is the body for PATCH /api/repos/{id}.
type PatchNotesRequest struct {
	Notes string `json:"notes" example:"interesting concurrency primitives"`
}

// Create tracks a new GitHub repository.
// @Summary Create repository
// @Description Fetch metadata from GitHub and add the repository to the watchlist.
// @Tags repos
// @Accept json
// @Produce json
// @Param body body CreateRepoRequest true "Repository owner and name"
// @Success 201 {object} service.RepoDTO
// @Failure 400 {object} apierror.Envelope
// @Failure 401 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 409 {object} apierror.Envelope
// @Failure 429 {object} apierror.Envelope
// @Failure 502 {object} apierror.Envelope
// @Failure 503 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos [post]
func (h *RepoHandler) Create(c *gin.Context) {
	var req CreateRepoRequest
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

// List returns a page of tracked repositories.
// @Summary List repositories
// @Description Cursor-paginated list with optional language filter and sort.
// @Tags repos
// @Produce json
// @Param language query string false "Filter by language"
// @Param sort query string false "Sort order" Enums(updated_desc,stars_desc)
// @Param cursor query string false "Opaque pagination cursor"
// @Param limit query int false "Page size (default 20, max 100)"
// @Success 200 {object} service.ListResult
// @Failure 400 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos [get]
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

// Get returns one tracked repository by id.
// @Summary Get repository
// @Tags repos
// @Produce json
// @Param id path string true "Repository ID" format(uuid)
// @Success 200 {object} service.RepoDTO
// @Failure 400 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos/{id} [get]
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

// Patch updates free-form notes on a repository.
// @Summary Update repository notes
// @Tags repos
// @Accept json
// @Produce json
// @Param id path string true "Repository ID" format(uuid)
// @Param body body PatchNotesRequest true "Notes payload"
// @Success 200 {object} service.RepoDTO
// @Failure 400 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos/{id} [patch]
func (h *RepoHandler) Patch(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	var req PatchNotesRequest
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

// Delete removes a repository from the watchlist.
// @Summary Delete repository
// @Tags repos
// @Param id path string true "Repository ID" format(uuid)
// @Success 204 "No Content"
// @Failure 400 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos/{id} [delete]
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

// Refresh re-fetches GitHub metadata for one repository.
// @Summary Refresh repository
// @Description Invalidate cache and re-fetch stars, language, description from GitHub.
// @Tags repos
// @Produce json
// @Param id path string true "Repository ID" format(uuid)
// @Success 200 {object} service.RepoDTO
// @Failure 400 {object} apierror.Envelope
// @Failure 401 {object} apierror.Envelope
// @Failure 404 {object} apierror.Envelope
// @Failure 429 {object} apierror.Envelope
// @Failure 502 {object} apierror.Envelope
// @Failure 503 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos/{id}/refresh [post]
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

// Stats returns aggregate watchlist metrics.
// @Summary Repository stats
// @Tags repos
// @Produce json
// @Success 200 {object} service.Stats
// @Failure 500 {object} apierror.Envelope
// @Router /repos/stats [get]
func (h *RepoHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	WriteJSON(c, http.StatusOK, stats)
}

// Changes returns an at-least-once change feed of updated repositories.
// @Summary Repository changes
// @Description Poll repositories with updated_at greater than the since cursor (duplicates OK).
// @Tags repos
// @Produce json
// @Param since query string false "Opaque changes cursor"
// @Param limit query int false "Page size (default 20, max 100)"
// @Success 200 {object} service.ChangesResult
// @Failure 400 {object} apierror.Envelope
// @Failure 500 {object} apierror.Envelope
// @Router /repos/changes [get]
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
