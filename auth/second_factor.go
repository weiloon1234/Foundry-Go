package auth

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// SecondFactor is a typed request-bound verification action, not an authenticated
// proof. Credential bindings call it within checked creation. A factor adapter
// must lock the current model, check eligibility, lock its factor, then call the
// pending-consumption callback once and verify/consume the submitted factor in
// that SAME transaction. No callback may commit, retry or escape its lifetime.
// Keep this value within the request; it contains an unverified factor response.
type SecondFactor[M model.Identifiable, K any] struct {
	provider Provider[M, K]
	verify   func(context.Context, *database.Tx, model.Reference[M, K], func(context.Context, *database.Tx) error) error
}

// DefineSecondFactor is a trusted adapter boundary. Applications normally use
// mfa.Factors.Verifier; defining a callback is not verification of client input.
func DefineSecondFactor[M model.Identifiable, K any](provider Provider[M, K], verify func(context.Context, *database.Tx, model.Reference[M, K], func(context.Context, *database.Tx) error) error) SecondFactor[M, K] {
	return SecondFactor[M, K]{provider: provider, verify: verify}
}
func (s SecondFactor[M, K]) ValidateFor(provider Provider[M, K]) error {
	if err := provider.Validate(); err != nil {
		return err
	}
	if err := s.provider.Validate(); err != nil {
		return err
	}
	if s.verify == nil || s.provider.definition.id != provider.definition.id {
		return fault.New(fault.Invalid, "second factor must share the credential provider declaration")
	}
	return nil
}
func (SecondFactor[M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("second-factor verification"))
}

// Check is the transactional credential-adapter boundary. It enforces one
// consumption callback in the same transaction or a nested savepoint. Omitted,
// repeated, substituted-transaction or suppressed failures never return success.
// Transaction correctness remains the adapter's contract; no inspection can
// undo a dishonest adapter's independently committed writes.
func (s SecondFactor[M, K]) Check(ctx context.Context, tx *database.Tx, provider Provider[M, K], identity model.Identity, consume func(context.Context, *database.Tx) error) error {
	if err := s.ValidateFor(provider); err != nil {
		return err
	}
	if ctx == nil || tx == nil || consume == nil {
		return fault.New(fault.Invalid, "second factor requires a transaction and pending consumption")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reference, err := provider.Parse(identity)
	if err != nil {
		return err
	}
	called := false
	var failure error
	err = callback.Isolated("second-factor verification", func() error {
		return s.verify(ctx, tx, reference, func(_ context.Context, child *database.Tx) error {
			if called || failure != nil || !tx.SharesTransaction(child) {
				if failure == nil {
					failure = fault.New(fault.Invalid, "second factor violated pending-consumption ownership")
				}
				return failure
			}
			called = true
			failure = callback.Isolated("pending credential consumption", func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return consume(ctx, child)
			})
			return failure
		})
	})
	if err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if !called {
		return fault.New(fault.Invalid, "second factor omitted pending credential consumption")
	}
	return ctx.Err()
}
