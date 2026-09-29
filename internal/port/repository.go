// Package port defines the interfaces (ports) that the service layer
// depends on. Concrete implementations (Postgres repositories, the crypto
// dice roller, in-memory fakes) live outside this package; the service
// layer never imports them directly.
package port

import (
	"context"

	"github.com/drzbraz/dice-bet/internal/domain"
)

// WalletRepository persists and retrieves wallet state. Lookup methods
// return (nil, nil) when the wallet does not exist; callers translate that
// into the appropriate domain error for their context.
type WalletRepository interface {
	// GetForUpdate reads the wallet row and, when running inside a
	// transaction started via TxManager, locks it with SELECT ... FOR
	// UPDATE so concurrent Play/EndPlay requests for the same client are
	// serialized even across multiple server instances.
	GetForUpdate(ctx context.Context, clientID string) (*domain.Wallet, error)
	Get(ctx context.Context, clientID string) (*domain.Wallet, error)
	// Update persists the wallet's current balance/version. May return a
	// *domain.Error with code INSUFFICIENT_BALANCE if the underlying
	// CHECK (balance >= 0) constraint rejects the write; this is a
	// defense-in-depth path since the domain layer already validates
	// balance before calling Update.
	Update(ctx context.Context, wallet *domain.Wallet) error
}

// PlayRepository persists and retrieves plays.
type PlayRepository interface {
	// Create inserts a new OPEN play. May return a *domain.Error with code
	// PLAY_ALREADY_IN_PROGRESS if the partial unique index on
	// (client_id) WHERE status = 'OPEN' rejects the insert; this is a
	// defense-in-depth path against a race the service layer's own check
	// did not catch.
	Create(ctx context.Context, play *domain.Play) error
	// GetOpenForUpdate returns the client's OPEN play, locked for update,
	// or (nil, nil) if none exists. The caller decides whether a present
	// or absent OPEN play is an error, since that depends on whether the
	// caller is starting or ending a play.
	GetOpenForUpdate(ctx context.Context, clientID string) (*domain.Play, error)
	Close(ctx context.Context, play *domain.Play) error
}

// TransactionRepository appends entries to the wallet ledger.
type TransactionRepository interface {
	Create(ctx context.Context, tx *domain.WalletTransaction) error
}

// IdempotencyRecord is a stored response for a previously executed request.
// RequestHash fingerprints the request's actual parameters (excluding
// clientID/requestID) so a replayed requestId whose parameters differ from
// the original request can be rejected instead of silently replaying a
// mismatched response.
type IdempotencyRecord struct {
	ClientID     string
	RequestID    string
	Operation    string
	RequestHash  string
	ResponseBody []byte
}

// IdempotencyRepository stores and retrieves idempotency records so that a
// repeated requestId for the same client returns the original response
// instead of re-executing the operation.
type IdempotencyRepository interface {
	// Get returns (nil, nil) if no record exists for (clientID, requestID).
	Get(ctx context.Context, clientID, requestID string) (*IdempotencyRecord, error)
	// Save persists a new idempotency record. If a concurrent request for
	// the same (clientID, requestID) committed first, implementations
	// return ErrIdempotencyConflict so the caller can retry the lookup.
	Save(ctx context.Context, record *IdempotencyRecord) error
}
