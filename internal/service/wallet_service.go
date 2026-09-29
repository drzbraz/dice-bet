// Package service implements the application's use cases (Wallet, Play,
// EndPlay) against the port interfaces. It contains no transport or
// persistence-specific code.
package service

import (
	"context"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
)

// WalletBalance is the transport-agnostic result of a wallet lookup.
type WalletBalance struct {
	ClientID string `json:"clientId"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// WalletService implements the Wallet use case: looking up a client's
// current balance. It is a pure read, so it does not participate in the
// idempotency scheme (which only guards mutating operations).
type WalletService struct {
	wallets port.WalletRepository
}

// NewWalletService constructs a WalletService.
func NewWalletService(wallets port.WalletRepository) *WalletService {
	return &WalletService{wallets: wallets}
}

// GetBalance returns the current balance for clientID, or a
// CLIENT_NOT_FOUND domain error if the client does not exist.
func (s *WalletService) GetBalance(ctx context.Context, clientID string) (*WalletBalance, error) {
	wallet, err := s.wallets.Get(ctx, clientID)
	if err != nil {
		return nil, wrapRepoErr(err)
	}
	if wallet == nil {
		return nil, domain.ErrClientNotFound(clientID)
	}
	return &WalletBalance{
		ClientID: wallet.ClientID,
		Balance:  wallet.Balance,
		Currency: wallet.Currency,
	}, nil
}
