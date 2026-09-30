package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func newTestSeed(clientID string) *domain.FairnessSeed {
	return &domain.FairnessSeed{
		ID:             uuid.NewString(),
		ClientID:       clientID,
		ServerSeed:     "server-seed-value",
		ServerSeedHash: domain.HashServerSeed("server-seed-value"),
		ClientSeed:     "client-seed-value",
		Nonce:          0,
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestFairnessRepository_GetActiveForUpdate_ReturnsNilWhenNoneExists(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)

	seed, err := repo.GetActiveForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	assert.Nil(t, seed)
}

func TestFairnessRepository_CreateThenGetActiveForUpdate_RoundTrips(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	want := newTestSeed("alice")

	require.NoError(t, repo.Create(context.Background(), want))

	got, err := repo.GetActiveForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.ServerSeed, got.ServerSeed)
	assert.Equal(t, want.ServerSeedHash, got.ServerSeedHash)
	assert.Equal(t, want.ClientSeed, got.ClientSeed)
	assert.Equal(t, int64(0), got.Nonce)
	assert.True(t, got.IsActive())
}

func TestFairnessRepository_Create_RejectsSecondActiveSeedForSameClient(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	require.NoError(t, repo.Create(context.Background(), newTestSeed("alice")))

	err := repo.Create(context.Background(), newTestSeed("alice"))

	require.Error(t, err)
}

func TestFairnessRepository_IncrementNonce_Persists(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	seed := newTestSeed("alice")
	require.NoError(t, repo.Create(context.Background(), seed))

	seed.Nonce = 5
	require.NoError(t, repo.IncrementNonce(context.Background(), seed))

	got, err := repo.GetActiveForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.Nonce)
}

func TestFairnessRepository_Retire_RevealsAndFreesUpANewActiveSeed(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	original := newTestSeed("alice")
	original.Nonce = 3
	require.NoError(t, repo.Create(context.Background(), original))
	require.NoError(t, repo.IncrementNonce(context.Background(), original))

	retired, err := repo.Retire(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, retired)
	assert.Equal(t, original.ID, retired.ID)
	assert.Equal(t, int64(3), retired.Nonce)
	assert.False(t, retired.IsActive())
	assert.NotNil(t, retired.RetiredAt)

	// No active seed remains, but a fresh one can now be created.
	active, err := repo.GetActiveForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	assert.Nil(t, active)

	require.NoError(t, repo.Create(context.Background(), newTestSeed("alice")))
	active, err = repo.GetActiveForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, active)
}

func TestFairnessRepository_Retire_ReturnsNilWhenNothingActive(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)

	retired, err := repo.Retire(context.Background(), "alice")
	require.NoError(t, err)
	assert.Nil(t, retired)
}

func TestFairnessRepository_ListRetired_NewestFirst(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newTestSeed("alice")))
	first, err := repo.Retire(ctx, "alice")
	require.NoError(t, err)

	require.NoError(t, repo.Create(ctx, newTestSeed("alice")))
	second, err := repo.Retire(ctx, "alice")
	require.NoError(t, err)

	list, err := repo.ListRetired(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, second.ID, list[0].ID, "newest retired seed must come first")
	assert.Equal(t, first.ID, list[1].ID)
}

func TestFairnessRepository_ListRetired_EmptyWhenNoneRetired(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)

	list, err := repo.ListRetired(context.Background(), "alice")
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestFairnessRepository_GetActiveForUpdate_LocksRowUntilCommit proves the
// same row-lock property GetForUpdate has for wallets: a concurrent
// GetActiveForUpdate for the same client blocks until the first
// transaction commits, which is what makes it safe to increment a seed's
// nonce alongside a Play's wallet debit without a lost update.
func TestFairnessRepository_GetActiveForUpdate_LocksRowUntilCommit(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewFairnessRepository(pool)
	require.NoError(t, repo.Create(context.Background(), newTestSeed("alice")))

	ctx := context.Background()
	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	ctx1 := context.WithValue(ctx, txContextKey{}, tx1)
	_, err = repo.GetActiveForUpdate(ctx1, "alice")
	require.NoError(t, err)

	blocked := make(chan struct{})
	unblocked := make(chan struct{})
	go func() {
		tx2, err := pool.Begin(context.Background())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback(context.Background()) }()
		ctx2 := context.WithValue(context.Background(), txContextKey{}, tx2)
		close(blocked)
		_, _ = repo.GetActiveForUpdate(ctx2, "alice")
		close(unblocked)
	}()

	<-blocked
	select {
	case <-unblocked:
		t.Fatal("expected second GetActiveForUpdate to block while the first transaction holds the row lock")
	case <-time.After(300 * time.Millisecond):
		// still blocked, as expected
	}

	require.NoError(t, tx1.Commit(ctx))

	select {
	case <-unblocked:
	case <-time.After(5 * time.Second):
		t.Fatal("expected second GetActiveForUpdate to unblock once the first transaction committed")
	}
}
