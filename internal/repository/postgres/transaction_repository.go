package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
)

// TransactionRepository implements port.TransactionRepository against
// Postgres, appending to the wallet_transactions ledger.
type TransactionRepository struct {
	pool *pgxpool.Pool
}

// NewTransactionRepository constructs a TransactionRepository.
func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{pool: pool}
}

var _ port.TransactionRepository = (*TransactionRepository)(nil)

// Create appends a ledger entry. UNIQUE (play_id, type) makes a duplicate
// debit or credit for the same play impossible; a violation here indicates
// a bug elsewhere (e.g. EndPlay somehow invoked twice for the same play
// despite the OPEN-play guard), so it is reported as an internal error
// rather than a recognized business condition.
func (r *TransactionRepository) Create(ctx context.Context, tx *domain.WalletTransaction) error {
	q := querierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO wallet_transactions (id, client_id, play_id, type, amount, balance_after, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tx.ID, tx.ClientID, tx.PlayID, tx.Type, tx.Amount, tx.BalanceAfter, tx.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCodeUniqueViolation && pgErr.ConstraintName == constraintLedgerUnique {
			return domain.ErrInternal(fmt.Errorf("duplicate ledger entry for play %s type %s: %w", tx.PlayID, tx.Type, err))
		}
		return fmt.Errorf("create wallet transaction: %w", err)
	}
	return nil
}
