// Package rest implements the Huma v2 / OpenAPI 3.1 HTTP REST adapter for ROMERO.
package rest

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

func mapAppError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, capture.ErrValidation),
		errors.Is(err, feedback.ErrValidation),
		errors.Is(err, retrieval.ErrValidation):
		return huma.Error422UnprocessableEntity("validation failed")
	case errors.Is(err, capture.ErrUnauthorized),
		errors.Is(err, feedback.ErrUnauthorized),
		errors.Is(err, retrieval.ErrUnauthorized):
		return huma.Error401Unauthorized("unauthorized")
	case errors.Is(err, capture.ErrForbidden),
		errors.Is(err, feedback.ErrForbidden),
		errors.Is(err, identity.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, capture.ErrNotFound),
		errors.Is(err, feedback.ErrNotFound),
		errors.Is(err, retrieval.ErrNotFound),
		errors.Is(err, memory.ErrNotFound):
		return huma.Error404NotFound("resource not found")
	case errors.Is(err, capture.ErrConflict),
		errors.Is(err, feedback.ErrConflict):
		return huma.Error409Conflict("conflict")
	case errors.Is(err, capture.ErrOperational),
		errors.Is(err, feedback.ErrOperational),
		errors.Is(err, retrieval.ErrOperational):
		return huma.Error503ServiceUnavailable("service temporarily unavailable")
	default:
		return huma.NewError(http.StatusInternalServerError, "internal server error")
	}
}
