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
// current balance.
//
// GetBalance reads cache-aside: a hit is served without touching the
// database at all -- the read that, at scale, would otherwise be routed to
// a (possibly lagging) replica. A miss falls through to the database and
// populates the cache for next time. The cache's write-through population
// after a successful Play/EndPlay (see GameService) is what keeps hits
// fresh; see the README for why that happens only after commit.
type WalletService struct {
	wallets port.WalletRepository
	cache   port.WalletBalanceCache
}

// NewWalletService constructs a WalletService.
func NewWalletService(wallets port.WalletRepository, cache port.WalletBalanceCache) *WalletService {
	return &WalletService{wallets: wallets, cache: cache}
}

// GetBalance returns the current balance for clientID, or a
// CLIENT_NOT_FOUND domain error if the client does not exist.
func (s *WalletService) GetBalance(ctx context.Context, clientID string) (*WalletBalance, error) {
	if balance, currency, ok := s.cache.Get(ctx, clientID); ok {
		return &WalletBalance{ClientID: clientID, Balance: balance, Currency: currency}, nil
	}

	wallet, err := s.wallets.Get(ctx, clientID)
	if err != nil {
		return nil, wrapRepoErr(err)
	}
	if wallet == nil {
		return nil, domain.ErrClientNotFound(clientID)
	}

	s.cache.Set(ctx, wallet.ClientID, wallet.Balance, wallet.Currency)
	return &WalletBalance{
		ClientID: wallet.ClientID,
		Balance:  wallet.Balance,
		Currency: wallet.Currency,
	}, nil
}

// ListClients returns every known client ID. There is no create-client use
// case in this project: clients are seeded via migration (see
// migrations/000002_seed_clients.up.sql) or inserted directly against the
// database, so this is a thin, uncached read used to populate a player
// picker rather than a full account-management endpoint.
func (s *WalletService) ListClients(ctx context.Context) ([]string, error) {
	ids, err := s.wallets.ListClientIDs(ctx)
	if err != nil {
		return nil, wrapRepoErr(err)
	}
	return ids, nil
}
