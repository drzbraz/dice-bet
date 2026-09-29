package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTxManager_CommitsOnSuccess(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	txManager := NewTxManager(pool)
	walletRepo := NewWalletRepository(pool)
	ctx := context.Background()

	err := txManager.WithinTx(ctx, func(ctx context.Context) error {
		w, err := walletRepo.GetForUpdate(ctx, "alice")
		if err != nil {
			return err
		}
		w.Balance = 700
		w.Version++
		return walletRepo.Update(ctx, w)
	})
	require.NoError(t, err)

	w, err := walletRepo.Get(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(700), w.Balance)
}

func TestTxManager_RollsBackOnError(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	txManager := NewTxManager(pool)
	walletRepo := NewWalletRepository(pool)
	ctx := context.Background()

	sentinel := errors.New("boom")
	err := txManager.WithinTx(ctx, func(ctx context.Context) error {
		w, err := walletRepo.GetForUpdate(ctx, "alice")
		if err != nil {
			return err
		}
		w.Balance = 1
		w.Version++
		if err := walletRepo.Update(ctx, w); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	w, err := walletRepo.Get(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), w.Balance, "the update must have been rolled back")
}
