package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (f *Factors[M, K]) withinTransaction(parent *database.Tx) (factorTransaction, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, fault.New(fault.Invalid, "MFA operation requires its owning transaction")
	}
	backend, ok := f.store.backend.(TransactionalBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "MFA backend cannot join a transaction")
	}
	return func(ctx context.Context, address Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error) {
		return backend.WithinIn(ctx, parent, address, identity, prepare, change)
	}, nil
}

// RetireIn is an explicitly authorized administrative operation for account
// retirement. It locks the existing model, removes its pending/confirmed factor,
// clears Enabled and invalidates every registered credential in the supplied
// transaction. Delete/retire the domain model in that same transaction AFTER this
// call. It accepts disabled accounts; it does not bypass a missing model or
// authenticate the caller. Enforce an administrative policy before invoking it.
//
// Unlike Disable this does not require the retiring user's password/factor or
// CanDisable permission. Never expose it as an unauthenticated self-service
// endpoint. Returned state is provisional until outer commit. Rollback restores
// model, factor and credentials. Do not reuse account keys without retirement.
func (f *Factors[M, K]) RetireIn(ctx context.Context, parent *database.Tx, reference model.Reference[M, K]) (M, error) {
	within, err := f.withinTransaction(parent)
	if err != nil {
		return *new(M), err
	}
	identity, err := reference.Identity()
	if err != nil {
		return *new(M), err
	}
	var result M
	err = f.mutate(ctx, identity, within, func(op context.Context, tx *database.Tx) (M, error) {
		if !parent.SharesTransaction(tx) {
			return *new(M), fault.New(fault.Invalid, "MFA backend substituted retirement transaction")
		}
		found, err := f.model.Lock(op, tx, reference.Key())
		if err != nil {
			return *new(M), err
		}
		current, present := found.Get()
		if !present {
			return *new(M), auth.Unauthenticated
		}
		actual, err := current.FoundryIdentity()
		if err != nil {
			return *new(M), err
		}
		if actual != identity {
			return *new(M), fault.New(fault.Invalid, "MFA retirement resolved a different model")
		}
		return current, nil
	}, func(op context.Context, tx *database.Tx, current M, _ value.Optional[Record], _ temporal.DateTime) (Change, error) {
		var err error
		result, err = f.setEnabled(op, tx, current, false)
		if err != nil {
			return Change{}, err
		}
		if err := f.changed(op, tx, identity, Retired); err != nil {
			return Change{}, err
		}
		return Remove(), nil
	})
	if err != nil {
		return *new(M), err
	}
	return result, nil
}
