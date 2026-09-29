package dto

import (
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/service"
)

// WalletGetRequest is the payload for a wallet.get request.
type WalletGetRequest struct {
	ClientID string `json:"clientId"`
}

var _ Validator = WalletGetRequest{}

func (r WalletGetRequest) Validate() error {
	if r.ClientID == "" {
		return domain.ErrValidation("clientId", "clientId is required")
	}
	return nil
}

// WalletGetResponse is the payload for a wallet.get.result response.
type WalletGetResponse struct {
	ClientID string `json:"clientId"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// NewWalletGetResponse maps a service.WalletBalance to its wire DTO.
func NewWalletGetResponse(b *service.WalletBalance) WalletGetResponse {
	return WalletGetResponse{
		ClientID: b.ClientID,
		Balance:  b.Balance,
		Currency: b.Currency,
	}
}
