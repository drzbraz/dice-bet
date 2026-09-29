package dto

import (
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/service"
)

// EndPlayRequest is the payload for a play.end request.
type EndPlayRequest struct {
	ClientID string `json:"clientId"`
}

var _ Validator = EndPlayRequest{}

func (r EndPlayRequest) Validate() error {
	if r.ClientID == "" {
		return domain.ErrValidation("clientId", "clientId is required")
	}
	return nil
}

// EndPlayResponse is the payload for a play.end.result response.
type EndPlayResponse struct {
	PlayID         string `json:"playId"`
	ClientID       string `json:"clientId"`
	Result         string `json:"result"`
	CreditedAmount int64  `json:"creditedAmount"`
	Status         string `json:"status"`
	Balance        int64  `json:"balance"`
}

// NewEndPlayResponse maps a service.EndPlayOutcome to its wire DTO.
func NewEndPlayResponse(o *service.EndPlayOutcome) EndPlayResponse {
	return EndPlayResponse{
		PlayID:         o.PlayID,
		ClientID:       o.ClientID,
		Result:         string(o.Result),
		CreditedAmount: o.CreditedAmount,
		Status:         string(o.Status),
		Balance:        o.Balance,
	}
}
