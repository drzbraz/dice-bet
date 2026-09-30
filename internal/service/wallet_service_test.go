package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/infrastructure/cache"
)

func TestWalletService_GetBalance_ReturnsBalance(t *testing.T) {
	h := newTestHarness(100, 10000)
	h.seedWallet("alice", 12345)

	out, err := h.wallet.GetBalance(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", out.ClientID)
	assert.Equal(t, int64(12345), out.Balance)
	assert.Equal(t, "EUR", out.Currency)
}

func TestWalletService_GetBalance_RejectsUnknownClient(t *testing.T) {
	h := newTestHarness(100, 10000)

	_, err := h.wallet.GetBalance(context.Background(), "ghost")
	requireDomainErr(t, err, domain.ErrCodeClientNotFound)
}

func TestWalletService_GetBalance_RepositoryErrorPropagatesAsInternal(t *testing.T) {
	svc := NewWalletService(&failingWalletRepository{err: errors.New("db down")}, cache.NewMemoryCache(time.Minute))

	_, err := svc.GetBalance(context.Background(), "alice")
	requireDomainErr(t, err, domain.ErrCodeInternal)
}

func TestWalletService_GetBalance_CacheHitNeverTouchesRepository(t *testing.T) {
	memCache := cache.NewMemoryCache(time.Minute)
	memCache.Set(context.Background(), "alice", 4200, "EUR")
	// A repository that errors if called at all, to prove a cache hit
	// short-circuits the read entirely rather than merely racing it.
	svc := NewWalletService(&failingWalletRepository{err: errors.New("must not be called on a cache hit")}, memCache)

	out, err := svc.GetBalance(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(4200), out.Balance)
	assert.Equal(t, "EUR", out.Currency)
}

func TestWalletService_GetBalance_MissPopulatesCacheForNextRead(t *testing.T) {
	h := newTestHarness(100, 10000)
	h.seedWallet("alice", 12345)

	_, err := h.wallet.GetBalance(context.Background(), "alice")
	require.NoError(t, err)

	balance, currency, ok := h.cache.Get(context.Background(), "alice")
	require.True(t, ok, "a cache miss must populate the cache for next time")
	assert.Equal(t, int64(12345), balance)
	assert.Equal(t, "EUR", currency)
}

func TestWalletService_ListClients_ReturnsAllClientIDsSorted(t *testing.T) {
	h := newTestHarness(100, 10000)
	h.seedWallet("carol", 100)
	h.seedWallet("alice", 200)
	h.seedWallet("bob", 300)

	ids, err := h.wallet.ListClients(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "bob", "carol"}, ids)
}

func TestWalletService_ListClients_EmptyWhenNoClientsSeeded(t *testing.T) {
	h := newTestHarness(100, 10000)

	ids, err := h.wallet.ListClients(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestWalletService_ListClients_RepositoryErrorPropagatesAsInternal(t *testing.T) {
	svc := NewWalletService(&failingWalletRepository{err: errors.New("db down")}, cache.NewMemoryCache(time.Minute))

	_, err := svc.ListClients(context.Background())
	requireDomainErr(t, err, domain.ErrCodeInternal)
}
