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

// PlayRepository implements port.PlayRepository against Postgres.
type PlayRepository struct {
	pool *pgxpool.Pool
}

// NewPlayRepository constructs a PlayRepository.
func NewPlayRepository(pool *pgxpool.Pool) *PlayRepository {
	return &PlayRepository{pool: pool}
}

var _ port.PlayRepository = (*PlayRepository)(nil)

// Create inserts a new OPEN play. The partial unique index
// uq_plays_one_open_per_client is the database's own defense-in-depth
// against a race the service layer's GetOpenForUpdate check did not catch;
// a violation maps to PLAY_ALREADY_IN_PROGRESS.
func (r *PlayRepository) Create(ctx context.Context, play *domain.Play) error {
	q := querierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO plays (id, client_id, bet_amount, bet_type, rolled_number, result, payout, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		play.ID, play.ClientID, play.BetAmount, play.BetType, play.RolledNumber, play.Result, play.Payout, play.Status, play.CreatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

// GetOpenForUpdate returns the client's OPEN play, locked for update, or
// (nil, nil) if none exists.
func (r *PlayRepository) GetOpenForUpdate(ctx context.Context, clientID string) (*domain.Play, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`SELECT id, client_id, bet_amount, bet_type, rolled_number, result, payout, status, created_at, closed_at
		 FROM plays WHERE client_id = $1 AND status = 'OPEN' FOR UPDATE`,
		clientID,
	)
	return scanPlay(row)
}

func scanPlay(row pgx.Row) (*domain.Play, error) {
	var p domain.Play
	if err := row.Scan(&p.ID, &p.ClientID, &p.BetAmount, &p.BetType, &p.RolledNumber, &p.Result, &p.Payout, &p.Status, &p.CreatedAt, &p.ClosedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan play: %w", err)
	}
	return &p, nil
}

// Close transitions the play to CLOSED. It only matches rows still OPEN,
// so a concurrent double-close affects zero rows on the loser.
func (r *PlayRepository) Close(ctx context.Context, play *domain.Play) error {
	q := querierFromContext(ctx, r.pool)
	tag, err := q.Exec(ctx,
		`UPDATE plays SET status = $1, closed_at = $2 WHERE id = $3 AND status = 'OPEN'`,
		play.Status, play.ClosedAt, play.ID,
	)
	if err != nil {
		return fmt.Errorf("close play: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNoActivePlay()
	}
	return nil
}
