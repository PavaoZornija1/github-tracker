package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/platform/logging"
	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
)

// RequestID is Gin middleware that ensures every request has an X-Request-ID
// in context, logs, and the response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestid.Header)
		if id == "" {
			id = requestid.New()
		}
		ctx := requestid.WithRequestID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set(requestid.Header, id)
		c.Next()
	}
}

// WriteError writes the standard error envelope. Unknown errors become 500s
// without leaking internal details in the message.
func WriteError(c *gin.Context, logger *slog.Logger, err error) {
	ctx := c.Request.Context()
	log := logging.FromContext(ctx, logger)

	var apiErr *apierror.Error
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 500 {
			log.Error("request failed", "err", err, "code", apiErr.Code)
		} else {
			log.Info("request rejected", "err", err, "code", apiErr.Code, "status", apiErr.Status)
		}
		c.AbortWithStatusJSON(apiErr.Status, apiErr.Envelope())
		return
	}

	log.Error("unhandled error", "err", err)
	internal := apierror.Internal("internal server error")
	c.AbortWithStatusJSON(http.StatusInternalServerError, internal.Envelope())
}

// WriteJSON is a small helper for successful JSON responses.
func WriteJSON(c *gin.Context, status int, body any) {
	c.JSON(status, body)
}
