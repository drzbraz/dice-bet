// Package http implements the REST mirror of the WebSocket use cases (for
// the Postman collection) plus /health, reusing the exact same services
// and DTOs as the WebSocket transport. It contains no business logic.
package http

import (
	"errors"
	"net/http"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/dto"
)

// statusByCode is the single source of truth for domain error code -> HTTP
// status mapping, per the error code table in the WebSocket contract.
var statusByCode = map[domain.ErrorCode]int{
	domain.ErrCodeInvalidMessage:        http.StatusBadRequest,
	domain.ErrCodeUnknownMessageType:    http.StatusNotFound,
	domain.ErrCodeValidationError:       http.StatusBadRequest,
	domain.ErrCodeInvalidBetAmount:      http.StatusUnprocessableEntity,
	domain.ErrCodeInvalidBetType:        http.StatusUnprocessableEntity,
	domain.ErrCodeClientNotFound:        http.StatusNotFound,
	domain.ErrCodeInsufficientBalance:   http.StatusUnprocessableEntity,
	domain.ErrCodePlayAlreadyInProgress: http.StatusConflict,
	domain.ErrCodeNoActivePlay:          http.StatusConflict,
	domain.ErrCodeServiceUnavailable:    http.StatusServiceUnavailable,
	domain.ErrCodeInternal:              http.StatusInternalServerError,
}

// mapError converts any error into an HTTP status and the ErrorDetail to
// embed in the JSON body. A non-domain error is never leaked to the
// client (no SQL/internal detail): it is reported as a 500 INTERNAL_ERROR.
func mapError(err error) (int, dto.ErrorDetail) {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		status, ok := statusByCode[domainErr.Code]
		if !ok {
			status = http.StatusInternalServerError
		}
		return status, dto.ErrorDetail{
			Code:    string(domainErr.Code),
			Message: domainErr.Message,
			Details: domainErr.Details,
		}
	}
	return http.StatusInternalServerError, dto.ErrorDetail{
		Code:    string(domain.ErrCodeInternal),
		Message: "internal error",
	}
}
