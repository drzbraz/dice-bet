package e2e

import (
	"context"
	"sync"
	"sync/atomic"
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

// TestFairness_HappyPath_RollsAreIndependentlyVerifiableAfterRotation runs
// the entire real stack (Postgres, row locks, HMAC derivation) through two
// plays and a rotation, then verifies every roll exactly the way an
// external player would: recompute domain.ComputeRoll from the now-
// revealed serverSeed and confirm it matches what Play actually returned.
func TestFairness_HappyPath_RollsAreIndependentlyVerifiableAfterRotation(t *testing.T) {
	pool := setupTestPool(t, 5)
	truncateAll(t, pool)
	seedClient(t, pool, "erin", 100_00)
	ctx := context.Background()

	fairnessRepo := postgres.NewFairnessRepository(pool)
	fairnessSvc := service.NewFairnessService(fairnessRepo, random.NewCryptoSeedGenerator(), postgres.NewTxManager(pool))
	walletCache := cache.NewMemoryCache(time.Minute)
	gameSvc := service.NewGameService(
		postgres.NewWalletRepository(pool),
		postgres.NewPlayRepository(pool),
		postgres.NewTransactionRepository(pool),
		postgres.NewIdempotencyRepository(pool),
		random.NewCryptoRoller(), // unused once fairness is wired: RollFor takes over
		postgres.NewTxManager(pool),
		walletCache,
		fairnessSvc,
		config.GameConfig{MinBet: 1, MaxBet: 100_00},
	)

	first, err := gameSvc.Play(ctx, service.PlayRequest{ClientID: "erin", RequestID: uuid.NewString(), BetAmount: 100, BetType: domain.BetTypeEven})
	require.NoError(t, err)
	_, err = gameSvc.EndPlay(ctx, service.EndPlayRequest{ClientID: "erin", RequestID: uuid.NewString()})
	require.NoError(t, err)

	second, err := gameSvc.Play(ctx, service.PlayRequest{ClientID: "erin", RequestID: uuid.NewString(), BetAmount: 100, BetType: domain.BetTypeOdd})
	require.NoError(t, err)
	_, err = gameSvc.EndPlay(ctx, service.EndPlayRequest{ClientID: "erin", RequestID: uuid.NewString()})
	require.NoError(t, err)

	require.NotNil(t, first.Fairness)
	require.NotNil(t, second.Fairness)
	assert.Equal(t, int64(0), first.Fairness.Nonce)
	assert.Equal(t, int64(1), second.Fairness.Nonce)
	assert.Equal(t, first.Fairness.ServerSeedHash, second.Fairness.ServerSeedHash, "same epoch, no rotation yet")

	// Rotate: reveals the serverSeed both plays above were derived from.
	rotated, err := fairnessSvc.RotateSeed(ctx, "erin", "")
	require.NoError(t, err)
	require.NotNil(t, rotated.Retired)
	assert.Equal(t, first.Fairness.ServerSeedHash, rotated.Retired.ServerSeedHash)

	// The self-consistency check: the revealed seed must hash back to
	// exactly what was published (implicitly, via Fairness.ServerSeedHash)
	// before either play happened.
	assert.Equal(t, rotated.Retired.ServerSeedHash, domain.HashServerSeed(rotated.Retired.ServerSeed))

	// And now, with the secret revealed, an outside verifier -- using
	// nothing but domain.ComputeRoll, no server access at all -- must
	// reproduce the exact rolls Play returned at the time.
	assert.Equal(t, first.RolledNumber, domain.ComputeRoll(rotated.Retired.ServerSeed, first.Fairness.ClientSeed, first.Fairness.Nonce))
	assert.Equal(t, second.RolledNumber, domain.ComputeRoll(rotated.Retired.ServerSeed, second.Fairness.ClientSeed, second.Fairness.Nonce))

	// History carries the same, fully revealed epoch.
	history, err := fairnessSvc.History(ctx, "erin")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, int64(2), history[0].FinalNonce, "two rolls were made (nonce 0 and 1) before rotation")
}

// TestFairness_Concurrency_NonceAdvancesExactlyOncePerSuccessfulPlay races
// the same client's Play the same way TestConcurrency_Play_ExactlyOneSucceedsPerClient
// does, and additionally asserts the fairness seed's nonce in real
// Postgres advances by exactly one -- proving the seed's row lock (via the
// same querierFromContext/ambient-transaction mechanism GetForUpdate uses
// for wallets) prevents a losing, rolled-back attempt from leaving a
// phantom nonce increment behind.
func TestFairness_Concurrency_NonceAdvancesExactlyOncePerSuccessfulPlay(t *testing.T) {
	pool := setupTestPool(t, concurrentGoroutines+5)
	truncateAll(t, pool)
	seedClient(t, pool, "frank", 1_000_000_00)
	ctx := context.Background()

	fairnessRepo := postgres.NewFairnessRepository(pool)
	fairnessSvc := service.NewFairnessService(fairnessRepo, random.NewCryptoSeedGenerator(), postgres.NewTxManager(pool))
	walletCache := cache.NewMemoryCache(time.Minute)
	gameSvc := service.NewGameService(
		postgres.NewWalletRepository(pool),
		postgres.NewPlayRepository(pool),
		postgres.NewTransactionRepository(pool),
		postgres.NewIdempotencyRepository(pool),
		random.NewCryptoRoller(),
		postgres.NewTxManager(pool),
		walletCache,
		fairnessSvc,
		config.GameConfig{MinBet: 1, MaxBet: 100_000_00},
	)

	var successes int32
	var wg sync.WaitGroup
	for i := 0; i < concurrentGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gameSvc.Play(context.Background(), service.PlayRequest{
				ClientID: "frank", RequestID: uuid.NewString(), BetAmount: 100, BetType: domain.BetTypeEven,
			})
			if err == nil {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), successes, "exactly one concurrent Play must succeed")

	seed, err := fairnessRepo.GetActiveForUpdate(ctx, "frank")
	require.NoError(t, err)
	require.NotNil(t, seed)
	assert.Equal(t, int64(1), seed.Nonce, "the nonce must have advanced by exactly one, matching the single successful play")
}
