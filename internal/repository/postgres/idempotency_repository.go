package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/drzbraz/dice-bet/internal/port"
)

// IdempotencyRepository implements port.IdempotencyRepository against
// Postgres.
type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

// NewIdempotencyRepository constructs an IdempotencyRepository.
func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

var _ port.IdempotencyRepository = (*IdempotencyRepository)(nil)

// Get returns (nil, nil) if no record exists for (clientID, requestID).
func (r *IdempotencyRepository) Get(ctx context.Context, clientID, requestID string) (*port.IdempotencyRecord, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`SELECT client_id, request_id, operation, request_hash, response_body FROM idempotency_keys WHERE client_id = $1 AND request_id = $2`,
		clientID, requestID,
	)
	var rec port.IdempotencyRecord
	if err := row.Scan(&rec.ClientID, &rec.RequestID, &rec.Operation, &rec.RequestHash, &rec.ResponseBody); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan idempotency record: %w", err)
	}
	return &rec, nil
}

// Save inserts a new idempotency record. If a concurrent request for the
// same (clientID, requestID) already committed, the PK violation is
// reported as port.ErrIdempotencyConflict so the caller can retry the
// lookup and return the winner's response instead.
func (r *IdempotencyRepository) Save(ctx context.Context, record *port.IdempotencyRecord) error {
	q := querierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO idempotency_keys (client_id, request_id, operation, request_hash, response_body) VALUES ($1, $2, $3, $4, $5)`,
		record.ClientID, record.RequestID, record.Operation, record.RequestHash, record.ResponseBody,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCodeUniqueViolation && pgErr.ConstraintName == constraintIdempotencyKeys {
			return port.ErrIdempotencyConflict
		}
		return fmt.Errorf("save idempotency record: %w", err)
	}
	return nil
}
