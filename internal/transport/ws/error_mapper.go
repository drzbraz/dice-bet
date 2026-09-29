package ws

import (
	"errors"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/dto"
)

// mapError converts any error returned by a handler into a WS error
// envelope's ErrorDetail. A non-domain error is never leaked to the
// client (no SQL/internal detail): it is reported as INTERNAL_ERROR with a
// generic message.
func mapError(err error) dto.ErrorDetail {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return dto.ErrorDetail{
			Code:    string(domainErr.Code),
			Message: domainErr.Message,
			Details: domainErr.Details,
		}
	}
	return dto.ErrorDetail{
		Code:    string(domain.ErrCodeInternal),
		Message: "internal error",
	}
}
