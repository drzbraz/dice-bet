package domain

import "time"

// TransactionType identifies a ledger entry kind. The UNIQUE(play_id, type)
// constraint on the persisted ledger makes a duplicate debit or credit for
// the same play impossible.
type TransactionType string

const (
	TransactionTypeBetDebit     TransactionType = "BET_DEBIT"
	TransactionTypePayoutCredit TransactionType = "PAYOUT_CREDIT"
)

// WalletTransaction is an append-only ledger entry recording a single
// balance change, for auditability independent of the wallet's current
// balance.
type WalletTransaction struct {
	ID           string
	ClientID     string
	PlayID       string
	Type         TransactionType
	Amount       int64
	BalanceAfter int64
	CreatedAt    time.Time
}
