package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func newFairnessTestHarness() (*FairnessService, *fakeFairnessRepository) {
	repo := newFakeFairnessRepository()
	svc := NewFairnessService(repo, &fakeSeedGenerator{}, &fakeTxManager{store: newFakeStore()})
	svc.now = fixedClock
	svc.newID = fixedIDGenerator()
	return svc, repo
}

func TestFairnessService_GetSeed_CreatesOnFirstCall(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	seed, err := svc.GetSeed(context.Background(), "alice")

	require.NoError(t, err)
	assert.Equal(t, "alice", seed.ClientID)
	assert.NotEmpty(t, seed.ServerSeedHash)
	assert.NotEmpty(t, seed.ClientSeed)
	assert.Equal(t, int64(0), seed.Nonce)
}

func TestFairnessService_GetSeed_ReturnsSameSeedOnSecondCall(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	first, err := svc.GetSeed(context.Background(), "alice")
	require.NoError(t, err)
	second, err := svc.GetSeed(context.Background(), "alice")
	require.NoError(t, err)

	assert.Equal(t, first.ServerSeedHash, second.ServerSeedHash)
	assert.Equal(t, first.ClientSeed, second.ClientSeed)
}

func TestFairnessService_GetSeed_DifferentClientsGetDifferentSeeds(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	alice, err := svc.GetSeed(context.Background(), "alice")
	require.NoError(t, err)
	bob, err := svc.GetSeed(context.Background(), "bob")
	require.NoError(t, err)

	assert.NotEqual(t, alice.ServerSeedHash, bob.ServerSeedHash)
}

func TestFairnessService_RotateSeed_FirstCallHasNoRetiredSeed(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	result, err := svc.RotateSeed(context.Background(), "alice", "")

	require.NoError(t, err)
	assert.Nil(t, result.Retired, "a client who never played has nothing to retire yet")
	assert.NotEmpty(t, result.Active.ServerSeedHash)
}

func TestFairnessService_RotateSeed_RevealsOldSeedAndActivatesNewOne(t *testing.T) {
	svc, _ := newFairnessTestHarness()
	ctx := context.Background()
	original, err := svc.GetSeed(ctx, "alice")
	require.NoError(t, err)

	result, err := svc.RotateSeed(ctx, "alice", "")

	require.NoError(t, err)
	require.NotNil(t, result.Retired)
	assert.Equal(t, original.ServerSeedHash, result.Retired.ServerSeedHash, "the revealed seed's commitment must match what was published before")
	assert.NotEmpty(t, result.Retired.ServerSeed, "the actual serverSeed must now be revealed")
	assert.NotEqual(t, result.Retired.ServerSeedHash, result.Active.ServerSeedHash, "the new epoch must have a different commitment")
}

// TestFairnessService_RotateSeed_RevealedSeedHashesToOriginallyPublishedHash
// is the self-consistency check that makes the whole scheme trustworthy: a
// player independently recomputes SHA256(revealed serverSeed) and confirms
// it equals the hash they were shown before ever placing a bet.
func TestFairnessService_RotateSeed_RevealedSeedHashesToOriginallyPublishedHash(t *testing.T) {
	svc, _ := newFairnessTestHarness()
	ctx := context.Background()
	published, err := svc.GetSeed(ctx, "alice")
	require.NoError(t, err)

	result, err := svc.RotateSeed(ctx, "alice", "")
	require.NoError(t, err)

	recomputedHash := domain.HashServerSeed(result.Retired.ServerSeed)
	assert.Equal(t, published.ServerSeedHash, recomputedHash)
}

func TestFairnessService_RotateSeed_WithCustomClientSeed_UsesIt(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	result, err := svc.RotateSeed(context.Background(), "alice", "my-chosen-seed")

	require.NoError(t, err)
	assert.Equal(t, "my-chosen-seed", result.Active.ClientSeed)
}

func TestFairnessService_History_EmptyBeforeAnyRotation(t *testing.T) {
	svc, _ := newFairnessTestHarness()

	history, err := svc.History(context.Background(), "alice")

	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestFairnessService_History_ReturnsRetiredSeedsNewestFirst(t *testing.T) {
	svc, _ := newFairnessTestHarness()
	ctx := context.Background()
	// A rotation with nothing active yet (the client never played) retires
	// nothing -- it only activates the first seed. So two epochs of
	// history need three rotations' worth of setup: an initial active
	// seed, then two rotations that each retire the previous one.
	initial, err := svc.GetSeed(ctx, "alice")
	require.NoError(t, err)

	first, err := svc.RotateSeed(ctx, "alice", "")
	require.NoError(t, err)
	require.NotNil(t, first.Retired)
	assert.Equal(t, initial.ServerSeedHash, first.Retired.ServerSeedHash)

	second, err := svc.RotateSeed(ctx, "alice", "")
	require.NoError(t, err)
	require.NotNil(t, second.Retired)
	assert.Equal(t, first.Active.ServerSeedHash, second.Retired.ServerSeedHash)

	history, err := svc.History(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, second.Retired.ServerSeedHash, history[0].ServerSeedHash, "newest retired seed must come first")
	assert.Equal(t, first.Retired.ServerSeedHash, history[1].ServerSeedHash)
}

func TestFairnessService_RollFor_IncrementsNonceSequentially(t *testing.T) {
	svc, _ := newFairnessTestHarness()
	ctx := context.Background()

	_, nonce0, _, _, err := svc.RollFor(ctx, "alice")
	require.NoError(t, err)
	_, nonce1, _, _, err := svc.RollFor(ctx, "alice")
	require.NoError(t, err)
	_, nonce2, _, _, err := svc.RollFor(ctx, "alice")
	require.NoError(t, err)

	assert.Equal(t, []int64{0, 1, 2}, []int64{nonce0, nonce1, nonce2})
}

func TestFairnessService_RollFor_MatchesIndependentComputeRoll(t *testing.T) {
	svc, repo := newFairnessTestHarness()
	ctx := context.Background()

	roll, nonce, hash, clientSeed, err := svc.RollFor(ctx, "alice")
	require.NoError(t, err)

	// Reconstruct the roll the way an external verifier would: read the
	// (now-active) seed directly and recompute via domain.ComputeRoll.
	seed, err := repo.GetActiveForUpdate(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, hash, seed.ServerSeedHash)
	assert.Equal(t, clientSeed, seed.ClientSeed)
	assert.Equal(t, roll, domain.ComputeRoll(seed.ServerSeed, seed.ClientSeed, nonce))
}

func TestFairnessService_RollFor_SameSeedProducesSameRollForSameNonce(t *testing.T) {
	svc, repo := newFairnessTestHarness()
	ctx := context.Background()
	roll, nonce, _, _, err := svc.RollFor(ctx, "alice")
	require.NoError(t, err)

	seed, err := repo.GetActiveForUpdate(ctx, "alice")
	require.NoError(t, err)

	// A verifier recomputing with the exact same (revealed) inputs must
	// get byte-for-byte the same roll, deterministically, with no access
	// to the server at all.
	assert.Equal(t, roll, domain.ComputeRoll(seed.ServerSeed, seed.ClientSeed, nonce))
}

func TestFairnessService_GetSeed_RepositoryErrorPropagatesAsInternal(t *testing.T) {
	svc := NewFairnessService(&failingFairnessRepository{err: errors.New("db down")}, &fakeSeedGenerator{}, &fakeTxManager{store: newFakeStore()})

	_, err := svc.GetSeed(context.Background(), "alice")

	requireDomainErr(t, err, domain.ErrCodeInternal)
}

func TestFairnessService_GetSeed_SeedGeneratorErrorPropagatesAsInternal(t *testing.T) {
	svc := NewFairnessService(newFakeFairnessRepository(), &failingSeedGenerator{err: errors.New("entropy source down")}, &fakeTxManager{store: newFakeStore()})

	_, err := svc.GetSeed(context.Background(), "alice")

	requireDomainErr(t, err, domain.ErrCodeInternal)
}

// failingFairnessRepository always fails, to test that repository errors
// propagate as INTERNAL_ERROR.
type failingFairnessRepository struct{ err error }

func (r *failingFairnessRepository) GetActiveForUpdate(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	return nil, r.err
}
func (r *failingFairnessRepository) Create(ctx context.Context, seed *domain.FairnessSeed) error {
	return r.err
}
func (r *failingFairnessRepository) IncrementNonce(ctx context.Context, seed *domain.FairnessSeed) error {
	return r.err
}
func (r *failingFairnessRepository) Retire(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	return nil, r.err
}
func (r *failingFairnessRepository) ListRetired(ctx context.Context, clientID string) ([]domain.FairnessSeed, error) {
	return nil, r.err
}
