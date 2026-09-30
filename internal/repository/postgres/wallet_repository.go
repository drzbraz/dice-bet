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

// WalletRepository implements port.WalletRepository against Postgres.
type WalletRepository struct {
	pool *pgxpool.Pool
}

// NewWalletRepository constructs a WalletRepository.
func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

var _ port.WalletRepository = (*WalletRepository)(nil)

// GetForUpdate reads and, when running inside a TxManager transaction,
// locks the wallet row with SELECT ... FOR UPDATE. This is the mechanism
// that serializes concurrent Play/EndPlay requests for the same client,
// even across multiple server instances, since the lock is held by
// Postgres rather than application memory.
func (r *WalletRepository) GetForUpdate(ctx context.Context, clientID string) (*domain.Wallet, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`SELECT client_id, balance, currency, version FROM wallets WHERE client_id = $1 FOR UPDATE`,
		clientID,
	)
	return scanWallet(row)
}

// Get reads the wallet without locking, for plain balance lookups.
func (r *WalletRepository) Get(ctx context.Context, clientID string) (*domain.Wallet, error) {
	q := querierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx,
		`SELECT client_id, balance, currency, version FROM wallets WHERE client_id = $1`,
		clientID,
	)
	return scanWallet(row)
}

func scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var w domain.Wallet
	if err := row.Scan(&w.ClientID, &w.Balance, &w.Currency, &w.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}
	return &w, nil
}

// Update persists the wallet's new balance/version. The domain layer
// (Wallet.Debit/Credit) has already incremented Version by one and
// validated the balance before calling Update; the WHERE clause on the
// pre-increment version plus the CHECK (balance >= 0) constraint are
// belt-and-suspenders guards, not the primary concurrency mechanism (that
// is GetForUpdate's row lock). See README assumptions.
func (r *WalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	q := querierFromContext(ctx, r.pool)
	tag, err := q.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = $2, updated_at = now() WHERE client_id = $3 AND version = $4`,
		wallet.Balance, wallet.Version, wallet.ClientID, wallet.Version-1,
	)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInternal(fmt.Errorf("wallet %s update affected no rows (missing row or concurrent modification outside the expected lock)", wallet.ClientID))
	}
	return nil
}

// ListClientIDs returns every client's ID, ordered alphabetically.
func (r *WalletRepository) ListClientIDs(ctx context.Context) ([]string, error) {
	q := querierFromContext(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT client_id FROM wallets ORDER BY client_id`)
	if err != nil {
		return nil, fmt.Errorf("list client ids: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan client id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list client ids: %w", err)
	}
	return ids, nil
}
