package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
)

// FairnessRepository implements port.FairnessRepository against Postgres.
type FairnessRepository struct {
	pool *pgxpool.Pool
}

// NewFairnessRepository constructs a FairnessRepository.
func NewFairnessRepository(pool *pgxpool.Pool) *FairnessRepository {
	return &FairnessRepository{pool: pool}
}

var _ port.FairnessRepository = (*FairnessRepository)(nil)

const fairnessSeedColumns = `id, client_id, server_seed, server_seed_hash, client_seed, nonce, created_at, retired_at`

// GetActiveForUpdate reads and, when running inside a TxManager
// transaction, locks the client's active seed row with SELECT ... FOR
// UPDATE -- the same mechanism GetForUpdate uses for wallets, so a seed's
// nonce increments safely alongside a Play's wallet debit in one
// transaction.
func (r *FairnessRepository) GetActiveForUpdate(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`SELECT `+fairnessSeedColumns+` FROM fairness_seeds WHERE client_id = $1 AND retired_at IS NULL FOR UPDATE`,
		clientID,
	)
	return scanFairnessSeed(row)
}

// Create inserts a new active seed. May return a *domain.Error with code
// FAIRNESS_DISABLED-adjacent internal error if the partial unique index on
// (client_id) WHERE retired_at IS NULL rejects the insert; this is a
// defense-in-depth path against a race the service layer's own
// get-or-create logic did not catch.
func (r *FairnessRepository) Create(ctx context.Context, seed *domain.FairnessSeed) error {
	q := querierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO fairness_seeds (id, client_id, server_seed, server_seed_hash, client_seed, nonce, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		seed.ID, seed.ClientID, seed.ServerSeed, seed.ServerSeedHash, seed.ClientSeed, seed.Nonce, seed.CreatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

// IncrementNonce persists seed.Nonce, which the caller has already
// incremented in memory after deriving a roll from the pre-increment
// value.
func (r *FairnessRepository) IncrementNonce(ctx context.Context, seed *domain.FairnessSeed) error {
	q := querierFromContext(ctx, r.pool)
	tag, err := q.Exec(ctx, `UPDATE fairness_seeds SET nonce = $1 WHERE id = $2`, seed.Nonce, seed.ID)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInternal(fmt.Errorf("fairness seed %s nonce update affected no rows", seed.ID))
	}
	return nil
}

// Retire marks the client's active seed retired (now safe to reveal) and
// returns it, or (nil, nil) if the client has no active seed.
func (r *FairnessRepository) Retire(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`UPDATE fairness_seeds SET retired_at = now()
		 WHERE client_id = $1 AND retired_at IS NULL
		 RETURNING `+fairnessSeedColumns,
		clientID,
	)
	return scanFairnessSeed(row)
}

// ListRetired returns clientID's retired seeds, newest first.
func (r *FairnessRepository) ListRetired(ctx context.Context, clientID string) ([]domain.FairnessSeed, error) {
	q := querierFromContext(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT `+fairnessSeedColumns+` FROM fairness_seeds WHERE client_id = $1 AND retired_at IS NOT NULL ORDER BY retired_at DESC`,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list retired fairness seeds: %w", err)
	}
	defer rows.Close()

	seeds := make([]domain.FairnessSeed, 0)
	for rows.Next() {
		seed, err := scanFairnessSeedRow(rows)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, *seed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list retired fairness seeds: %w", err)
	}
	return seeds, nil
}

// fairnessSeedScanner is satisfied by both pgx.Row and pgx.Rows.
type fairnessSeedScanner interface {
	Scan(dest ...any) error
}

func scanFairnessSeed(row pgx.Row) (*domain.FairnessSeed, error) {
	seed, err := scanFairnessSeedRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan fairness seed: %w", err)
	}
	return seed, nil
}

func scanFairnessSeedRow(row fairnessSeedScanner) (*domain.FairnessSeed, error) {
	var s domain.FairnessSeed
	if err := row.Scan(&s.ID, &s.ClientID, &s.ServerSeed, &s.ServerSeedHash, &s.ClientSeed, &s.Nonce, &s.CreatedAt, &s.RetiredAt); err != nil {
		return nil, err
	}
	return &s, nil
}
