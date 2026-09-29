package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier is the subset of pgx.Tx / pgxpool.Pool that repositories use,
// letting them run either inside a transaction or directly against the
// pool without knowing which.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// querierFromContext returns the transaction stored in ctx by TxManager,
// if present, otherwise falls back to the pool.
func querierFromContext(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return pool
}
