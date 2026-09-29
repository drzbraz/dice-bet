package port

import "context"

// TxManager runs fn inside a single database transaction. Implementations
// store the transaction handle in the returned context so repositories can
// pick it up transparently; if fn returns an error the transaction is
// rolled back, otherwise it is committed.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
