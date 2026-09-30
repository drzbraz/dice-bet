package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCache_MissWhenNeverSet(t *testing.T) {
	c := NewMemoryCache(time.Minute)

	_, _, ok := c.Get(context.Background(), "alice")
	assert.False(t, ok)
}

func TestMemoryCache_SetThenGet_Hits(t *testing.T) {
	c := NewMemoryCache(time.Minute)

	c.Set(context.Background(), "alice", 1000, "EUR")

	balance, currency, ok := c.Get(context.Background(), "alice")
	require.True(t, ok)
	assert.Equal(t, int64(1000), balance)
	assert.Equal(t, "EUR", currency)
}

func TestMemoryCache_Invalidate_RemovesEntry(t *testing.T) {
	c := NewMemoryCache(time.Minute)
	c.Set(context.Background(), "alice", 1000, "EUR")

	c.Invalidate(context.Background(), "alice")

	_, _, ok := c.Get(context.Background(), "alice")
	assert.False(t, ok)
}

func TestMemoryCache_ExpiresAfterTTL(t *testing.T) {
	c := NewMemoryCache(time.Minute)
	current := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return current }

	c.Set(context.Background(), "alice", 1000, "EUR")

	current = current.Add(59 * time.Second)
	_, _, ok := c.Get(context.Background(), "alice")
	assert.True(t, ok, "must still be fresh just under the TTL")

	current = current.Add(2 * time.Second)
	_, _, ok = c.Get(context.Background(), "alice")
	assert.False(t, ok, "must be expired just past the TTL")
}

func TestMemoryCache_DifferentClientsDoNotCollide(t *testing.T) {
	c := NewMemoryCache(time.Minute)
	c.Set(context.Background(), "alice", 1000, "EUR")
	c.Set(context.Background(), "bob", 500, "USD")

	balance, currency, ok := c.Get(context.Background(), "bob")
	require.True(t, ok)
	assert.Equal(t, int64(500), balance)
	assert.Equal(t, "USD", currency)

	balance, currency, ok = c.Get(context.Background(), "alice")
	require.True(t, ok)
	assert.Equal(t, int64(1000), balance)
	assert.Equal(t, "EUR", currency)
}
