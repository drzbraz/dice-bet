package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/infrastructure/cache"
	"github.com/drzbraz/dice-bet/internal/infrastructure/random"
	"github.com/drzbraz/dice-bet/internal/repository/postgres"
	"github.com/drzbraz/dice-bet/internal/service"
)

// TestHappyPath_WalletPlayEndPlayWallet exercises the full stack (real
// service wiring against a real Postgres instance) through the sequence
// the Postman collection's Happy Path folder also drives: Wallet -> Play
// -> EndPlay -> Wallet.
func TestHappyPath_WalletPlayEndPlayWallet(t *testing.T) {
	pool := setupTestPool(t, 5)
	truncateAll(t, pool)
	const startingBalance = int64(100_00)
	seedClient(t, pool, "carol", startingBalance)

	walletCache := cache.NewMemoryCache(time.Minute)
	walletSvc := service.NewWalletService(postgres.NewWalletRepository(pool), walletCache)
	gameSvc := service.NewGameService(
		postgres.NewWalletRepository(pool),
		postgres.NewPlayRepository(pool),
		postgres.NewTransactionRepository(pool),
		postgres.NewIdempotencyRepository(pool),
		random.NewCryptoRoller(),
		postgres.NewTxManager(pool),
		walletCache,
		config.GameConfig{MinBet: 1, MaxBet: 100_00},
	)
	ctx := context.Background()

	initial, err := walletSvc.GetBalance(ctx, "carol")
	require.NoError(t, err)
	assert.Equal(t, startingBalance, initial.Balance)

	playOut, err := gameSvc.Play(ctx, service.PlayRequest{
		ClientID: "carol", RequestID: uuid.NewString(), BetAmount: 1000, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)
	assert.Equal(t, startingBalance-1000, playOut.Balance)
	assert.Equal(t, domain.PlayStatusOpen, playOut.Status)

	endOut, err := gameSvc.EndPlay(ctx, service.EndPlayRequest{ClientID: "carol", RequestID: uuid.NewString()})
	require.NoError(t, err)
	assert.Equal(t, domain.PlayStatusClosed, endOut.Status)
	assert.Equal(t, playOut.Payout, endOut.CreditedAmount)
	assert.Equal(t, startingBalance-1000+playOut.Payout, endOut.Balance)

	final, err := walletSvc.GetBalance(ctx, "carol")
	require.NoError(t, err)
	assert.Equal(t, endOut.Balance, final.Balance)
}
