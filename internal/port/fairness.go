package port

import (
	"context"

	"github.com/drzbraz/dice-bet/internal/domain"
)

// SeedGenerator produces the random inputs a fresh FairnessSeed needs. Kept
// separate from DiceRoller: DiceRoller produces a roll directly, while this
// produces the secret material that ComputeRoll later derives many rolls
// from deterministically.
type SeedGenerator interface {
	GenerateServerSeed() (string, error)
	GenerateClientSeed() (string, error)
}

// FairnessRepository persists FairnessSeed epochs. Exactly one seed per
// client is active (RetiredAt == nil) at a time; GetActiveForUpdate locks
// that row within the caller's transaction the same way
// WalletRepository.GetForUpdate does, so a seed's nonce can be incremented
// safely alongside a Play's wallet debit in the same transaction.
type FairnessRepository interface {
	// GetActiveForUpdate returns (nil, nil) if the client has no active
	// seed yet (e.g. they have never played).
	GetActiveForUpdate(ctx context.Context, clientID string) (*domain.FairnessSeed, error)
	Create(ctx context.Context, seed *domain.FairnessSeed) error
	// IncrementNonce persists seed.Nonce, which the caller has already
	// incremented in memory.
	IncrementNonce(ctx context.Context, seed *domain.FairnessSeed) error
	// Retire marks the client's active seed retired (revealing it is now
	// safe) and returns it, or (nil, nil) if none was active.
	Retire(ctx context.Context, clientID string) (*domain.FairnessSeed, error)
	// ListRetired returns clientID's retired seeds, newest first.
	ListRetired(ctx context.Context, clientID string) ([]domain.FairnessSeed, error)
}
