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

func newOpenPlay(clientID string) *domain.Play {
	return &domain.Play{
		ID:           uuid.NewString(),
		ClientID:     clientID,
		BetAmount:    500,
		BetType:      domain.BetTypeEven,
		RolledNumber: 4,
		Result:       domain.PlayResultWin,
		Payout:       1000,
		Status:       domain.PlayStatusOpen,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestPlayRepository_CreateAndGetOpenForUpdate(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewPlayRepository(pool)
	ctx := context.Background()

	play := newOpenPlay("alice")
	require.NoError(t, repo.Create(ctx, play))

	got, err := repo.GetOpenForUpdate(ctx, "alice")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, play.ID, got.ID)
	assert.Equal(t, domain.PlayStatusOpen, got.Status)
	assert.Nil(t, got.ClosedAt)
}

func TestPlayRepository_GetOpenForUpdate_ReturnsNilWhenNoneOpen(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewPlayRepository(pool)

	got, err := repo.GetOpenForUpdate(context.Background(), "alice")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPlayRepository_Create_RejectsSecondOpenPlayViaPartialUniqueIndex(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewPlayRepository(pool)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newOpenPlay("alice")))

	err := repo.Create(ctx, newOpenPlay("alice"))
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodePlayAlreadyInProgress, domainErr.Code)
}

func TestPlayRepository_Close_TransitionsToClosed(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewPlayRepository(pool)
	ctx := context.Background()

	play := newOpenPlay("alice")
	require.NoError(t, repo.Create(ctx, play))

	closedAt := time.Now().UTC().Truncate(time.Microsecond)
	play.Close(closedAt)
	require.NoError(t, repo.Close(ctx, play))

	got, err := repo.GetOpenForUpdate(ctx, "alice")
	require.NoError(t, err)
	assert.Nil(t, got, "no OPEN play should remain")
}

func TestPlayRepository_Close_AlreadyClosedReturnsNoActivePlay(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewPlayRepository(pool)
	ctx := context.Background()

	play := newOpenPlay("alice")
	require.NoError(t, repo.Create(ctx, play))
	play.Close(time.Now().UTC())
	require.NoError(t, repo.Close(ctx, play))

	err := repo.Close(ctx, play)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeNoActivePlay, domainErr.Code)
}

func TestPlayRepository_TwoDifferentClientsCanEachHaveAnOpenPlay(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	seedClient(t, pool, "bob", 1000)
	repo := NewPlayRepository(pool)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newOpenPlay("alice")))
	require.NoError(t, repo.Create(ctx, newOpenPlay("bob")))

	aliceOpen, err := repo.GetOpenForUpdate(ctx, "alice")
	require.NoError(t, err)
	assert.NotNil(t, aliceOpen)

	bobOpen, err := repo.GetOpenForUpdate(ctx, "bob")
	require.NoError(t, err)
	assert.NotNil(t, bobOpen)
}
