package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
)

func gameConfigFor(minBet, maxBet int64) config.GameConfig {
	return config.GameConfig{MinBet: minBet, MaxBet: maxBet}
}

var fixedTime = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return fixedTime }

func requireDomainErr(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr), "expected a *domain.Error, got %T: %v", err, err)
	assert.Equal(t, code, domainErr.Code)
}

func TestGameService_Play_WinDebitsAndReturnsOutcome(t *testing.T) {
	h := newTestHarness(100, 10000, 2) // roll=2 -> EVEN wins
	h.seedWallet("alice", 1000)

	out, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, out.RolledNumber)
	assert.Equal(t, domain.PlayResultWin, out.Result)
	assert.Equal(t, int64(1000), out.Payout) // 2x bet
	assert.Equal(t, domain.PlayStatusOpen, out.Status)
	assert.Equal(t, int64(500), out.Balance) // debited immediately

	wallet, err := h.wallets.Get(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(500), wallet.Balance)

	play, err := h.plays.GetOpenForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, play)
	assert.Equal(t, domain.PlayStatusOpen, play.Status)
	assert.Len(t, h.store.ledger, 1)
	assert.Equal(t, domain.TransactionTypeBetDebit, h.store.ledger[0].Type)
}

func TestGameService_Play_LoseDebitsAndReturnsZeroPayout(t *testing.T) {
	h := newTestHarness(100, 10000, 3) // roll=3 odd -> EVEN loses
	h.seedWallet("alice", 1000)

	out, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.PlayResultLose, out.Result)
	assert.Equal(t, int64(0), out.Payout)
	assert.Equal(t, int64(500), out.Balance)
}

func TestGameService_Play_AllFacesAndBetTypes(t *testing.T) {
	for roll := 1; roll <= 6; roll++ {
		for _, betType := range []domain.BetType{domain.BetTypeEven, domain.BetTypeOdd} {
			roll, betType := roll, betType
			t.Run("", func(t *testing.T) {
				h := newTestHarness(1, 10000, roll)
				h.seedWallet("alice", 1000)
				out, err := h.game.Play(context.Background(), PlayRequest{
					ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: betType,
				})
				require.NoError(t, err)
				wantWin := domain.IsWin(roll, betType)
				if wantWin {
					assert.Equal(t, domain.PlayResultWin, out.Result)
					assert.Equal(t, int64(200), out.Payout)
				} else {
					assert.Equal(t, domain.PlayResultLose, out.Result)
					assert.Equal(t, int64(0), out.Payout)
				}
			})
		}
	}
}

func TestGameService_Play_RejectsWhileOpenPlayExists(t *testing.T) {
	h := newTestHarness(100, 10000, 2, 4)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	_, err = h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-2", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodePlayAlreadyInProgress)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(900), wallet.Balance, "second play must not debit")
}

func TestGameService_Play_RejectsBetExceedingBalance(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 100)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeInsufficientBalance)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(100), wallet.Balance)
}

func TestGameService_Play_RejectsInvalidBetAmount(t *testing.T) {
	cases := []struct {
		name string
		bet  int64
	}{
		{"zero", 0},
		{"negative", -100},
		{"below min", 10},
		{"above max", 100000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHarness(50, 5000, 2)
			h.seedWallet("alice", 100000)
			_, err := h.game.Play(context.Background(), PlayRequest{
				ClientID: "alice", RequestID: "req-1", BetAmount: tc.bet, BetType: domain.BetTypeEven,
			})
			requireDomainErr(t, err, domain.ErrCodeInvalidBetAmount)
		})
	}
}

func TestGameService_Play_RejectsInvalidBetType(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: "PRIME",
	})
	requireDomainErr(t, err, domain.ErrCodeInvalidBetType)
}

func TestGameService_Play_RejectsUnknownClient(t *testing.T) {
	h := newTestHarness(100, 10000, 2)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "ghost", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeClientNotFound)
}

func TestGameService_Play_IdempotentReplayReturnsIdenticalResponseNoSideEffects(t *testing.T) {
	h := newTestHarness(100, 10000, 2, 4, 6) // extra rolls would be consumed if replayed
	h.seedWallet("alice", 1000)

	first, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	second, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)
	assert.Equal(t, first, second)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(900), wallet.Balance, "replay must not debit again")
	assert.Len(t, h.store.ledger, 1, "replay must not create a second ledger entry")
	assert.Equal(t, 1, h.roller.i, "replay must not roll the die again")
}

func TestGameService_Play_IdempotentReplayAcrossOperationsRejected(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "shared-id", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	_, err = h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "shared-id"})
	requireDomainErr(t, err, domain.ErrCodeValidationError)
}

func TestGameService_Play_ReplayWithDifferentParamsRejected(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	_, err = h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 200, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeValidationError)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(900), wallet.Balance, "the rejected replay must not debit again")
}

func TestGameService_Play_ConflictingConcurrentIdempotencySaveRetriesAndReturnsWinner(t *testing.T) {
	// Two rolls: the fake conflict rolls back the first attempt's business
	// logic entirely (including its roll), so the retry rolls again.
	h := newTestHarness(100, 10000, 2, 2)
	h.seedWallet("alice", 1000)
	h.idempotency.simulateConflictOnce("alice", "req-1")

	out, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(900), out.Balance)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(900), wallet.Balance, "the rolled-back losing attempt must not double-debit")
	assert.Len(t, h.store.ledger, 1)
}

func TestGameService_Play_RollbackOnMidTransactionFailure(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)
	// Pre-create a ledger entry that will collide with the one Play tries
	// to insert, forcing a failure after the wallet has already been
	// debited in-memory, to prove the tx manager rolls the debit back.
	h.store.ledger = append(h.store.ledger, domain.WalletTransaction{
		PlayID: "collide", Type: domain.TransactionTypeBetDebit,
	})
	origNewID := h.game.newID
	h.game.newID = func() string { return "collide" }
	defer func() { h.game.newID = origNewID }()

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeInternal)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(1000), wallet.Balance, "failed transaction must not leave a partial debit")

	play, _ := h.plays.GetOpenForUpdate(context.Background(), "alice")
	assert.Nil(t, play, "failed transaction must not leave a partial play")
}

func TestGameService_Play_RepositoryErrorPropagatesAsInternal(t *testing.T) {
	store := newFakeStore()
	failingWallets := &failingWalletRepository{err: errors.New("connection reset")}
	txManager := &fakeTxManager{store: store}
	idem := &fakeIdempotencyRepository{store: store}
	roller := &fakeDiceRoller{rolls: []int{2}}
	svc := NewGameService(failingWallets, &fakePlayRepository{store: store}, &fakeTransactionRepository{store: store}, idem, roller, txManager, gameConfigFor(1, 10000))

	_, err := svc.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeInternal)
}

func TestGameService_EndPlay_CreditsOnWinAndClosesPlay(t *testing.T) {
	h := newTestHarness(100, 10000, 2) // win
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	out, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	require.NoError(t, err)
	assert.Equal(t, domain.PlayResultWin, out.Result)
	assert.Equal(t, int64(1000), out.CreditedAmount)
	assert.Equal(t, domain.PlayStatusClosed, out.Status)
	assert.Equal(t, int64(1500), out.Balance) // 500 remaining + 1000 payout

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(1500), wallet.Balance)
	assert.Len(t, h.store.closedPlays["alice"], 1)
}

func TestGameService_EndPlay_CreditsZeroOnLoss(t *testing.T) {
	h := newTestHarness(100, 10000, 3) // odd roll, EVEN bet loses
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	out, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	require.NoError(t, err)
	assert.Equal(t, domain.PlayResultLose, out.Result)
	assert.Equal(t, int64(0), out.CreditedAmount)
	assert.Equal(t, int64(500), out.Balance)
}

func TestGameService_EndPlay_RejectsWithoutOpenPlay(t *testing.T) {
	h := newTestHarness(100, 10000)
	h.seedWallet("alice", 1000)

	_, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-1"})
	requireDomainErr(t, err, domain.ErrCodeNoActivePlay)
}

func TestGameService_EndPlay_CalledTwiceNeverCreditsTwice(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	first, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	require.NoError(t, err)
	assert.Equal(t, int64(1500), first.Balance)

	_, err = h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-3"})
	requireDomainErr(t, err, domain.ErrCodeNoActivePlay)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(1500), wallet.Balance, "balance must not change on the second EndPlay")
}

func TestGameService_EndPlay_IdempotentReplayReturnsIdenticalResponseNoSideEffects(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)

	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	first, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	require.NoError(t, err)

	second, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	require.NoError(t, err)
	assert.Equal(t, first, second)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(1500), wallet.Balance, "replay must not credit again")
}

func TestGameService_EndPlay_RejectsUnknownClient(t *testing.T) {
	h := newTestHarness(100, 10000)

	_, err := h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "ghost", RequestID: "req-1"})
	requireDomainErr(t, err, domain.ErrCodeClientNotFound)
}

func TestGameService_EndPlay_WalletUpdateFailurePropagatesAsInternal(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)
	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	failingWallets := &updateFailingWalletRepository{fakeWalletRepository: h.wallets, err: errors.New("write timeout")}
	svc := NewGameService(failingWallets, h.plays, h.txs, h.idempotency, h.roller, h.txManager, gameConfigFor(100, 10000))
	svc.now = fixedClock
	svc.idemBackoff = func(context.Context, int) {}

	_, err = svc.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	requireDomainErr(t, err, domain.ErrCodeInternal)

	play, _ := h.plays.GetOpenForUpdate(context.Background(), "alice")
	assert.NotNil(t, play, "failed transaction must not leave the play closed")
}

func TestGameService_EndPlay_PlayCloseFailurePropagatesAsInternal(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)
	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	failingPlays := &closeFailingPlayRepository{fakePlayRepository: h.plays, err: errors.New("write timeout")}
	svc := NewGameService(h.wallets, failingPlays, h.txs, h.idempotency, h.roller, h.txManager, gameConfigFor(100, 10000))
	svc.now = fixedClock
	svc.idemBackoff = func(context.Context, int) {}

	_, err = svc.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	requireDomainErr(t, err, domain.ErrCodeInternal)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(500), wallet.Balance, "failed transaction must not leave a partial credit")
}

func TestGameService_EndPlay_LedgerFailureRollsBackCredit(t *testing.T) {
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)
	_, err := h.game.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 500, BetType: domain.BetTypeEven,
	})
	require.NoError(t, err)

	play, err := h.plays.GetOpenForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	h.store.ledger = append(h.store.ledger, domain.WalletTransaction{
		PlayID: play.ID, Type: domain.TransactionTypePayoutCredit,
	})

	_, err = h.game.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-2"})
	requireDomainErr(t, err, domain.ErrCodeInternal)

	wallet, _ := h.wallets.Get(context.Background(), "alice")
	assert.Equal(t, int64(500), wallet.Balance, "failed transaction must not leave a partial credit")
	stillOpen, _ := h.plays.GetOpenForUpdate(context.Background(), "alice")
	assert.NotNil(t, stillOpen, "failed transaction must not leave the play closed")
}

func TestGameService_Play_IdempotencyGetFailurePropagatesAsInternal(t *testing.T) {
	store := newFakeStore()
	h := newTestHarness(100, 10000, 2)
	h.seedWallet("alice", 1000)
	svc := NewGameService(h.wallets, h.plays, h.txs, &erroringIdempotencyRepository{err: errors.New("db down")}, h.roller, &fakeTxManager{store: store}, gameConfigFor(100, 10000))

	_, err := svc.Play(context.Background(), PlayRequest{
		ClientID: "alice", RequestID: "req-1", BetAmount: 100, BetType: domain.BetTypeEven,
	})
	requireDomainErr(t, err, domain.ErrCodeInternal)
}

func TestGameService_EndPlay_IdempotencyGetFailurePropagatesAsInternal(t *testing.T) {
	store := newFakeStore()
	svc := NewGameService(&fakeWalletRepository{store: store}, &fakePlayRepository{store: store}, &fakeTransactionRepository{store: store}, &erroringIdempotencyRepository{err: errors.New("db down")}, &fakeDiceRoller{}, &fakeTxManager{store: store}, gameConfigFor(100, 10000))

	_, err := svc.EndPlay(context.Background(), EndPlayRequest{ClientID: "alice", RequestID: "req-1"})
	requireDomainErr(t, err, domain.ErrCodeInternal)
}
