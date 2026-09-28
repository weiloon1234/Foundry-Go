package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type factorTransaction func(context.Context, Address, model.Identity, func(context.Context, *database.Tx) error, func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error)

func (f *Factors[M, K]) withPassword(ctx context.Context, password auth.PasswordResult[M, K], action func(context.Context, *database.Tx, M, value.Optional[Record], temporal.DateTime) (Change, error)) error {
	if err := f.Validate(); err != nil {
		return err
	}
	return f.mutate(ctx, password.Proof().Identity(), f.store.backend.Within, func(op context.Context, tx *database.Tx) (M, error) {
		return f.provider.RecheckPassword(op, tx, password)
	}, action)
}

func (f *Factors[M, K]) mutate(ctx context.Context, identity model.Identity, within factorTransaction, load func(context.Context, *database.Tx) (M, error), action func(context.Context, *database.Tx, M, value.Optional[Record], temporal.DateTime) (Change, error)) error {
	if err := f.Validate(); err != nil {
		return err
	}
	return f.store.gate.Execute(ctx, func(op context.Context) error {
		if _, err := f.provider.Parse(identity); err != nil {
			return err
		}
		var current M
		var owned *database.Tx
		var expected value.Optional[Record]
		prepared, changed := false, false
		var callbackError error
		invalid := func() error {
			if callbackError == nil {
				callbackError = fault.New(fault.Invalid, "MFA backend violated callback ownership")
			}
			return callbackError
		}
		actual, err := within(op, f.address, identity, func(_ context.Context, tx *database.Tx) error {
			if prepared || changed || tx == nil || callbackError != nil {
				return invalid()
			}
			prepared = true
			owned = tx
			callbackError = callback.Isolated("MFA model preparation", func() error {
				var err error
				current, err = load(op, tx)
				return err
			})
			return callbackError
		}, func(_ context.Context, tx *database.Tx, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
			if !prepared || changed || tx != owned || callbackError != nil {
				return Change{}, invalid()
			}
			changed = true
			if !validInstant(now) {
				callbackError = fault.New(fault.Invalid, "MFA backend returned an invalid clock")
				return Change{}, callbackError
			}
			if record, present := stored.Get(); present {
				if callbackError = record.Validate(f.address, identity); callbackError != nil {
					return Change{}, callbackError
				}
				if now.UTC().Before(record.CreatedAt.UTC()) {
					callbackError = auth.Unauthenticated
					return Change{}, callbackError
				}
				stored = value.Set(record.Clone())
			}
			var change Change
			callbackError = callback.Isolated("MFA factor mutation", func() error {
				var err error
				change, err = action(op, tx, current, stored, now)
				if err != nil {
					return err
				}
				if err := change.Validate(f.address, identity); err != nil {
					return err
				}
				return op.Err()
			})
			if callbackError != nil {
				return Change{}, callbackError
			}

			expected = change.Next()
			return change, nil
		})
		if err != nil {
			return err
		}
		if callbackError != nil {
			return callbackError
		}
		if !prepared || !changed {
			return invalid()
		}
		want, wantPresent := expected.Get()
		got, gotPresent := actual.Get()
		if wantPresent != gotPresent || wantPresent && !want.same(got) {
			return fault.New(fault.Invalid, "MFA backend returned a different mutation")
		}
		return op.Err()
	})
}
