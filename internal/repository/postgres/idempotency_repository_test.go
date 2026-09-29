package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/port"
)

func TestIdempotencyRepository_Get_ReturnsNilWhenMissing(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewIdempotencyRepository(pool)

	rec, err := repo.Get(context.Background(), "alice", "req-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestIdempotencyRepository_SaveThenGet_RoundTrips(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewIdempotencyRepository(pool)
	ctx := context.Background()

	record := &port.IdempotencyRecord{
		ClientID:     "alice",
		RequestID:    "req-1",
		Operation:    "play.start",
		ResponseBody: []byte(`{"balance":500}`),
	}
	require.NoError(t, repo.Save(ctx, record))

	got, err := repo.Get(ctx, "alice", "req-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, record.Operation, got.Operation)
	assert.JSONEq(t, string(record.ResponseBody), string(got.ResponseBody))
}

func TestIdempotencyRepository_Save_RejectsDuplicateKeyAsConflict(t *testing.T) {
	pool := setupTestPool(t)
	truncateAll(t, pool)
	seedClient(t, pool, "alice", 1000)
	repo := NewIdempotencyRepository(pool)
	ctx := context.Background()

	record := &port.IdempotencyRecord{ClientID: "alice", RequestID: "req-1", Operation: "play.start", ResponseBody: []byte(`{}`)}
	require.NoError(t, repo.Save(ctx, record))

	err := repo.Save(ctx, record)
	require.True(t, errors.Is(err, port.ErrIdempotencyConflict))
}
