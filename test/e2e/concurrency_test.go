package e2e

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/infrastructure/random"
	"github.com/drzbraz/dice-bet/internal/repository/postgres"
	"github.com/drzbraz/dice-bet/internal/service"
)

const concurrentGoroutines = 20

func TestConcurrency_Play_ExactlyOneSucceedsPerClient(t *testing.T) {
	pool := setupTestPool(t, concurrentGoroutines+5)
	truncateAll(t, pool)
	const startingBalance = int64(1_000_000_00)
	seedClient(t, pool, "alice", startingBalance)

	wallets := postgres.NewWalletRepository(pool)
	gameSvc := service.NewGameService(
		wallets,
		postgres.NewPlayRepository(pool),
		postgres.NewTransactionRepository(pool),
		postgres.NewIdempotencyRepository(pool),
		random.NewCryptoRoller(),
		postgres.NewTxManager(pool),
		config.GameConfig{MinBet: 1, MaxBet: 100_000_00},
	)

	var successes, conflicts, unexpected int32
	var wg sync.WaitGroup
	for i := 0; i < concurrentGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gameSvc.Play(context.Background(), service.PlayRequest{
				ClientID: "alice", RequestID: uuid.NewString(), BetAmount: 100, BetType: domain.BetTypeEven,
			})
			switch {
			case err == nil:
				atomic.AddInt32(&successes, 1)
			case isDomainCode(err, domain.ErrCodePlayAlreadyInProgress):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Logf("unexpected Play error: %v", err)
				atomic.AddInt32(&unexpected, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), successes, "exactly one concurrent Play must succeed")
	assert.Equal(t, int32(concurrentGoroutines-1), conflicts, "every other concurrent Play must be rejected as PLAY_ALREADY_IN_PROGRESS")
	assert.Equal(t, int32(0), unexpected)

	wallet, err := wallets.Get(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, startingBalance-100, wallet.Balance, "balance must reflect exactly one debit")

	var ledgerCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM wallet_transactions WHERE client_id = 'alice' AND type = 'BET_DEBIT'`,
	).Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount, "exactly one BET_DEBIT ledger row must exist")
}

func TestConcurrency_EndPlay_CreditedExactlyOnce(t *testing.T) {
	pool := setupTestPool(t, concurrentGoroutines+5)
	truncateAll(t, pool)
	const startingBalance = int64(1_000_000_00)
	seedClient(t, pool, "bob", startingBalance)

	wallets := postgres.NewWalletRepository(pool)
	gameSvc := service.NewGameService(
		wallets,
		postgres.NewPlayRepository(pool),
		postgres.NewTransactionRepository(pool),
		postgres.NewIdempotencyRepository(pool),
		random.NewCryptoRoller(),
		postgres.NewTxManager(pool),
		config.GameConfig{MinBet: 1, MaxBet: 100_000_00},
	)

	playOut, err := gameSvc.Play(context.Background(), service.PlayRequest{
		ClientID: "bob", RequestID: uuid.NewString(), BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	var successes, noActive, unexpected int32
	var wg sync.WaitGroup
	for i := 0; i < concurrentGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gameSvc.EndPlay(context.Background(), service.EndPlayRequest{
				ClientID: "bob", RequestID: uuid.NewString(),
			})
			switch {
			case err == nil:
				atomic.AddInt32(&successes, 1)
			case isDomainCode(err, domain.ErrCodeNoActivePlay):
				atomic.AddInt32(&noActive, 1)
			default:
				t.Logf("unexpected EndPlay error: %v", err)
				atomic.AddInt32(&unexpected, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), successes, "exactly one concurrent EndPlay must succeed")
	assert.Equal(t, int32(concurrentGoroutines-1), noActive, "every other concurrent EndPlay must be rejected as NO_ACTIVE_PLAY")
	assert.Equal(t, int32(0), unexpected)

	wallet, err := wallets.Get(context.Background(), "bob")
	require.NoError(t, err)
	assert.Equal(t, startingBalance-100+playOut.Payout, wallet.Balance, "payout must be credited exactly once")

	var ledgerCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM wallet_transactions WHERE client_id = 'bob' AND type = 'PAYOUT_CREDIT'`,
	).Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount, "exactly one PAYOUT_CREDIT ledger row must exist")
}

func isDomainCode(err error, code domain.ErrorCode) bool {
	var domainErr *domain.Error
	return errors.As(err, &domainErr) && domainErr.Code == code
}
