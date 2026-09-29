package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/drzbraz/dice-bet/internal/domain"
)

// Postgres error codes used for domain error mapping. Mapping is always
// done via pgconn.PgError.Code/ConstraintName, never by matching error
// message strings.
const (
	pgCodeUniqueViolation = "23505"
	pgCodeCheckViolation  = "23514"
)

const (
	constraintUniqueOpenPlay  = "uq_plays_one_open_per_client"
	constraintWalletBalance   = "wallets_balance_check"
	constraintLedgerUnique    = "wallet_transactions_play_id_type_key"
	constraintIdempotencyKeys = "idempotency_keys_pkey"
)

// mapPgError translates a Postgres constraint violation into the
// corresponding domain error. It returns the original error unchanged if
// it does not recognize a mapped constraint, so the caller can wrap it as
// an internal error.
func mapPgError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == pgCodeUniqueViolation && pgErr.ConstraintName == constraintUniqueOpenPlay:
		return domain.ErrPlayAlreadyInProgress()
	case pgErr.Code == pgCodeCheckViolation && pgErr.ConstraintName == constraintWalletBalance:
		return domain.ErrInsufficientBalance()
	default:
		return err
	}
}
