// Package cache provides a process-local implementation of
// port.WalletBalanceCache. In a horizontally-scaled deployment (more than
// one server instance) this would need to be a shared cache such as Redis
// instead, so every instance sees the same fresh values -- a process-local
// cache only protects reads served by the same instance that did the
// write. See the README for the full reasoning.
package cache

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	balance  int64
	currency string
	expires  time.Time
}

// MemoryCache is an in-memory, process-local implementation of
// port.WalletBalanceCache.
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
	now     func() time.Time
}

// NewMemoryCache constructs a MemoryCache. ttl bounds how long a written
// value is trusted without a fresh write -- since every wallet mutation
// writes through on commit, this is a defense-in-depth backstop (e.g.
// against a value written by a path that bypassed the cache) rather than
// the primary freshness mechanism.
func NewMemoryCache(ttl time.Duration) *MemoryCache {
	return &MemoryCache{entries: make(map[string]entry), ttl: ttl, now: time.Now}
}

func (c *MemoryCache) Get(_ context.Context, clientID string) (int64, string, bool) {
	c.mu.RLock()
	e, ok := c.entries[clientID]
	c.mu.RUnlock()
	if !ok || c.now().After(e.expires) {
		return 0, "", false
	}
	return e.balance, e.currency, true
}

func (c *MemoryCache) Set(_ context.Context, clientID string, balance int64, currency string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[clientID] = entry{balance: balance, currency: currency, expires: c.now().Add(c.ttl)}
}

func (c *MemoryCache) Invalidate(_ context.Context, clientID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, clientID)
}
