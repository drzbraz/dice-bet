package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestWalletRepository_Get_ReturnsNilForUnknownClient(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	repo := NewWalletRepository(pool)

	w, err := repo.Get(context.Background(), "ghost")
	require.NoError(t, err)
	assert.Nil(t, w)
}

func TestWalletRepository_Get_ReturnsSeededWallet(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 12345)
	repo := NewWalletRepository(pool)

	w, err := repo.Get(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.Equal(t, "alice", w.ClientID)
	assert.Equal(t, int64(12345), w.Balance)
	assert.Equal(t, "EUR", w.Currency)
	assert.Equal(t, int64(0), w.Version)
}

func TestWalletRepository_Update_PersistsBalanceAndVersion(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewWalletRepository(pool)
	ctx := context.Background()

	w, err := repo.Get(ctx, "alice")
	require.NoError(t, err)
	w.Balance = 400
	w.Version++

	require.NoError(t, repo.Update(ctx, w))

	reloaded, err := repo.Get(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(400), reloaded.Balance)
	assert.Equal(t, int64(1), reloaded.Version)
}

func TestWalletRepository_Update_RejectsNegativeBalanceViaCheckConstraint(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 100)
	repo := NewWalletRepository(pool)
	ctx := context.Background()

	w, err := repo.Get(ctx, "alice")
	require.NoError(t, err)
	// Bypass the domain layer's own guard to prove the database CHECK
	// constraint is real defense-in-depth, not just documentation.
	w.Balance = -50
	w.Version++

	err = repo.Update(ctx, w)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInsufficientBalance, domainErr.Code)

	reloaded, _ := repo.Get(ctx, "alice")
	assert.Equal(t, int64(100), reloaded.Balance, "the rejected update must not have applied")
}

func TestWalletRepository_Update_MissingRowReturnsInternalError(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	repo := NewWalletRepository(pool)

	err := repo.Update(context.Background(), &domain.Wallet{ClientID: "ghost", Balance: 100, Version: 1})
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInternal, domainErr.Code)
}

func TestWalletRepository_GetForUpdate_LocksRowUntilCommit(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewWalletRepository(pool)
	ctx := context.Background()

	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	ctx1 := context.WithValue(ctx, txContextKey{}, tx1)
	_, err = repo.GetForUpdate(ctx1, "alice")
	require.NoError(t, err)

	blocked := make(chan struct{})
	unblocked := make(chan struct{})
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			close(blocked)
			close(unblocked)
			return
		}
		defer tx2.Rollback(context.Background())
		ctx2 := context.WithValue(ctx, txContextKey{}, tx2)
		close(blocked)
		_, _ = repo.GetForUpdate(ctx2, "alice")
		close(unblocked)
	}()

	<-blocked
	select {
	case <-unblocked:
		t.Fatal("expected second GetForUpdate to block while the first transaction holds the row lock")
	case <-time.After(300 * time.Millisecond):
		// still blocked, as expected
	}

	require.NoError(t, tx1.Commit(ctx))

	select {
	case <-unblocked:
	case <-time.After(5 * time.Second):
		t.Fatal("expected second GetForUpdate to unblock once the first transaction committed")
	}
}
