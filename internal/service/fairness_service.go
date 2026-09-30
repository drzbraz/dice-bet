package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
)

// FairnessSeedView is the transport-agnostic, safe-to-publish view of a
// client's active fairness seed: the commitment (ServerSeedHash), never
// the secret ServerSeed itself.
type FairnessSeedView struct {
	ClientID       string `json:"clientId"`
	ServerSeedHash string `json:"serverSeedHash"`
	ClientSeed     string `json:"clientSeed"`
	Nonce          int64  `json:"nonce"`
}

// RetiredFairnessSeedView is a fully revealed, past seed epoch: everything
// needed to independently recompute and verify every roll made under it
// (nonces 0 through FinalNonce-1).
type RetiredFairnessSeedView struct {
	ClientID       string    `json:"clientId"`
	ServerSeed     string    `json:"serverSeed"`
	ServerSeedHash string    `json:"serverSeedHash"`
	ClientSeed     string    `json:"clientSeed"`
	FinalNonce     int64     `json:"finalNonce"`
	RetiredAt      time.Time `json:"retiredAt"`
}

// RotateSeedResult is the outcome of rotating a client's active seed:
// the just-retired seed (nil if the client had never played before) and
// the newly active one.
type RotateSeedResult struct {
	Retired *RetiredFairnessSeedView `json:"retired,omitempty"`
	Active  FairnessSeedView         `json:"active"`
}

// FairnessService implements the provably-fair use cases: publishing a
// client's current commitment, rotating to a new seed (revealing the old
// one), and listing past epochs for re-verification. GameService.Play
// depends on it too, via RollFor, to actually derive rolls when the
// PROVABLY_FAIR_ENABLED feature is on -- see README "Provably fair rolls".
type FairnessService struct {
	repo  port.FairnessRepository
	seeds port.SeedGenerator
	tx    port.TxManager

	now   func() time.Time
	newID func() string
}

// NewFairnessService constructs a FairnessService.
func NewFairnessService(repo port.FairnessRepository, seeds port.SeedGenerator, tx port.TxManager) *FairnessService {
	return &FairnessService{
		repo:  repo,
		seeds: seeds,
		tx:    tx,
		now:   time.Now,
		newID: uuid.NewString,
	}
}

// GetSeed returns clientID's current commitment, creating a fresh seed on
// first use. Safe to call before any bet: ServerSeedHash is the public
// commitment, ServerSeed itself is never included.
func (s *FairnessService) GetSeed(ctx context.Context, clientID string) (*FairnessSeedView, error) {
	var seed *domain.FairnessSeed
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		seed, err = s.getOrCreateActive(ctx, clientID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return toSeedView(seed), nil
}

// RotateSeed retires clientID's active seed -- revealing it, so every roll
// made under it becomes independently verifiable -- and immediately
// activates a new one. newClientSeed overrides the default generated
// client seed for the new epoch when non-empty, letting a player
// contribute entropy the server could not have chosen around.
func (s *FairnessService) RotateSeed(ctx context.Context, clientID, newClientSeed string) (*RotateSeedResult, error) {
	var result RotateSeedResult
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		retired, err := s.repo.Retire(ctx, clientID)
		if err != nil {
			return wrapRepoErr(err)
		}
		if retired != nil {
			result.Retired = toRetiredView(retired)
		}

		active, err := s.createActive(ctx, clientID, newClientSeed)
		if err != nil {
			return err
		}
		result.Active = *toSeedView(active)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// History returns clientID's retired seeds, newest first, each fully
// revealed and ready to re-verify.
func (s *FairnessService) History(ctx context.Context, clientID string) ([]RetiredFairnessSeedView, error) {
	seeds, err := s.repo.ListRetired(ctx, clientID)
	if err != nil {
		return nil, wrapRepoErr(err)
	}
	views := make([]RetiredFairnessSeedView, 0, len(seeds))
	for i := range seeds {
		views = append(views, *toRetiredView(&seeds[i]))
	}
	return views, nil
}

// RollFor derives clientID's next roll from their active seed (creating
// one on first use) and persists the incremented nonce. It does not open
// its own transaction: callers -- only GameService.Play, never a
// transport directly -- already run inside one via TxManager, and the
// nonce increment must commit or roll back atomically with that play's
// wallet debit, exactly like the wallet row lock it runs alongside.
func (s *FairnessService) RollFor(ctx context.Context, clientID string) (roll int, nonce int64, serverSeedHash, clientSeed string, err error) {
	seed, err := s.getOrCreateActive(ctx, clientID)
	if err != nil {
		return 0, 0, "", "", err
	}
	nonce = seed.Nonce
	roll = domain.ComputeRoll(seed.ServerSeed, seed.ClientSeed, nonce)
	seed.Nonce++
	if err := s.repo.IncrementNonce(ctx, seed); err != nil {
		return 0, 0, "", "", wrapRepoErr(err)
	}
	return roll, nonce, seed.ServerSeedHash, seed.ClientSeed, nil
}

// getOrCreateActive returns clientID's active seed, locked for update,
// creating one if none exists yet. Callers are responsible for running
// this inside a transaction (either their own, via WithinTx, or one
// already ambient from an enclosing caller like GameService.Play).
func (s *FairnessService) getOrCreateActive(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	seed, err := s.repo.GetActiveForUpdate(ctx, clientID)
	if err != nil {
		return nil, wrapRepoErr(err)
	}
	if seed != nil {
		return seed, nil
	}
	return s.createActive(ctx, clientID, "")
}

func (s *FairnessService) createActive(ctx context.Context, clientID, clientSeedOverride string) (*domain.FairnessSeed, error) {
	serverSeed, err := s.seeds.GenerateServerSeed()
	if err != nil {
		return nil, domain.ErrInternal(err)
	}
	clientSeed := clientSeedOverride
	if clientSeed == "" {
		clientSeed, err = s.seeds.GenerateClientSeed()
		if err != nil {
			return nil, domain.ErrInternal(err)
		}
	}
	seed := &domain.FairnessSeed{
		ID:             s.newID(),
		ClientID:       clientID,
		ServerSeed:     serverSeed,
		ServerSeedHash: domain.HashServerSeed(serverSeed),
		ClientSeed:     clientSeed,
		Nonce:          0,
		CreatedAt:      s.now(),
	}
	if err := s.repo.Create(ctx, seed); err != nil {
		return nil, wrapRepoErr(err)
	}
	return seed, nil
}

func toSeedView(seed *domain.FairnessSeed) *FairnessSeedView {
	return &FairnessSeedView{
		ClientID:       seed.ClientID,
		ServerSeedHash: seed.ServerSeedHash,
		ClientSeed:     seed.ClientSeed,
		Nonce:          seed.Nonce,
	}
}

func toRetiredView(seed *domain.FairnessSeed) *RetiredFairnessSeedView {
	var retiredAt time.Time
	if seed.RetiredAt != nil {
		retiredAt = *seed.RetiredAt
	}
	return &RetiredFairnessSeedView{
		ClientID:       seed.ClientID,
		ServerSeed:     seed.ServerSeed,
		ServerSeedHash: seed.ServerSeedHash,
		ClientSeed:     seed.ClientSeed,
		FinalNonce:     seed.Nonce,
		RetiredAt:      retiredAt,
	}
}
