package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestTransactionRepository_Create_Success(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	playRepo := NewPlayRepository(pool)
	txRepo := NewTransactionRepository(pool)
	ctx := context.Background()

	play := newOpenPlay("alice")
	require.NoError(t, playRepo.Create(ctx, play))

	entry := &domain.WalletTransaction{
		ID:           uuid.NewString(),
		ClientID:     "alice",
		PlayID:       play.ID,
		Type:         domain.TransactionTypeBetDebit,
		Amount:       500,
		BalanceAfter: 500,
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, txRepo.Create(ctx, entry))
}

func TestTransactionRepository_Create_RejectsDuplicateLedgerEntryForSamePlayAndType(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	playRepo := NewPlayRepository(pool)
	txRepo := NewTransactionRepository(pool)
	ctx := context.Background()

	play := newOpenPlay("alice")
	require.NoError(t, playRepo.Create(ctx, play))

	entry := func() *domain.WalletTransaction {
		return &domain.WalletTransaction{
			ID:           uuid.NewString(),
			ClientID:     "alice",
			PlayID:       play.ID,
			Type:         domain.TransactionTypeBetDebit,
			Amount:       500,
			BalanceAfter: 500,
			CreatedAt:    time.Now().UTC(),
		}
	}

	require.NoError(t, txRepo.Create(ctx, entry()))

	err := txRepo.Create(ctx, entry())
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInternal, domainErr.Code)
}
