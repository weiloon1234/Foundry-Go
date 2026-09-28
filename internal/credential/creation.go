package credential

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// CheckCreation ensures a synchronous adapter called the supplied check exactly
// once and did not suppress its failure. Adapter transaction correctness remains
// its contract; metadata inspection cannot undo a misbehaving adapter's commit.
func CheckCreation[T any](ctx context.Context, check func(context.Context, *database.Tx) error, create func(func(context.Context, *database.Tx) error) (T, error)) (T, error) {
	called := false
	var failure error
	result, err := create(func(op context.Context, tx *database.Tx) error {
		if called {
			failure = fault.New(fault.Invalid, "credential backend repeated its issuance check")
			return failure
		}
		called = true
		if err := ctx.Err(); err != nil {
			failure = err
			return err
		}
		failure = check(op, tx)
		return failure
	})
	if err != nil {
		return *new(T), err
	}
	if !called {
		return *new(T), fault.New(fault.Invalid, "credential backend omitted its issuance check")
	}
	if failure != nil {
		return *new(T), failure
	}
	if err := ctx.Err(); err != nil {
		return *new(T), err
	}
	return result, nil
}
