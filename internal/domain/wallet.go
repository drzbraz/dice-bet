package domain

import (
	"errors"
	"math"
)

// Wallet holds a client's balance in minor currency units (e.g. cents).
// Money is always int64; floats must never be used for monetary values.
type Wallet struct {
	ClientID string
	Balance  int64
	Currency string
	Version  int64
}

// Debit subtracts amount from the balance. amount must be positive and must
// not exceed the current balance. Callers are expected to have already
// validated the bet amount against MIN_BET/MAX_BET before calling Debit;
// a non-positive amount here indicates a programming error upstream.
func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return ErrInternal(errors.New("debit amount must be positive"))
	}
	if amount > w.Balance {
		return ErrInsufficientBalance()
	}
	w.Balance -= amount
	w.Version++
	return nil
}

// Credit adds amount to the balance. amount must be non-negative (a losing
// play credits 0). Guards against int64 overflow, which is otherwise
// unreachable given configured bet limits but is defended against here as
// the domain invariant boundary.
func (w *Wallet) Credit(amount int64) error {
	if amount < 0 {
		return ErrInternal(errors.New("credit amount must not be negative"))
	}
	if amount > math.MaxInt64-w.Balance {
		return ErrInternal(errors.New("wallet balance overflow"))
	}
	w.Balance += amount
	w.Version++
	return nil
}
