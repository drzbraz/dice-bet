package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/drzbraz/dice-bet/internal/port"
)

type txContextKey struct{}

// TxManager implements port.TxManager against a pgxpool.Pool, storing the
// active pgx.Tx in the context so repositories can pick it up
// transparently via querierFromContext.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager constructs a TxManager.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

var _ port.TxManager = (*TxManager)(nil)

// WithinTx begins a transaction, runs fn with the transaction stored in
// its context, and commits on success or rolls back on error.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w (rollback also failed: %w)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}
