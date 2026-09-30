package port

import "context"

// WalletBalanceCache is a small write-through cache for wallet balances.
//
// It exists to demonstrate a specific, deliberate fix for a real class of
// bug: once reads are served from a database replica that lags behind the
// primary (the usual way to scale read throughput), a read immediately
// after a write can be routed to a replica that hasn't caught up yet and
// silently return stale data. Populating the cache with the fresh,
// just-committed value at write time -- rather than merely invalidating it
// -- means a read immediately after a write is served from the cache
// without ever touching the (possibly lagging) replica at all. See the
// "Read freshness under replica lag" section of the README for the full
// reasoning, including why writes are populated only after the owning
// transaction commits.
type WalletBalanceCache interface {
	// Get returns the cached balance for clientID, or ok=false on a miss
	// (never seen, or expired).
	Get(ctx context.Context, clientID string) (balance int64, currency string, ok bool)
	// Set writes balance/currency through to the cache. Callers must only
	// pass a value that is already durably committed -- never a value
	// that was read or computed inside a transaction that might still
	// roll back, or the cache could end up holding a value the database
	// never actually had.
	Set(ctx context.Context, clientID string, balance int64, currency string)
	// Invalidate removes a cached entry. Used when a value is known to be
	// stale but the fresh value isn't available to write through.
	Invalidate(ctx context.Context, clientID string)
}
