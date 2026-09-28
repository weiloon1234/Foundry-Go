package database

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Transactor owns a callback's atomic transaction scope. DB and Session open a
// transaction; Tx creates a savepoint within its existing transaction. Custom
// implementations must wait for callback completion and preserve outcome errors.
type Transactor interface {
	Transaction(context.Context, func(*Tx) error, ...TxOptions) error
}

// Transaction runs a nested callback in a savepoint. Isolation and read-only
// settings belong to the outer transaction, so nested options are rejected.
// Successful return releases the savepoint; only the outer scope can commit.
func (tx *Tx) Transaction(ctx context.Context, fn func(*Tx) error, options ...TxOptions) error {
	if len(options) != 0 {
		return fault.New(fault.Invalid, "nested transaction options must be set on the outer transaction")
	}
	return tx.Savepoint(ctx, fn)
}
