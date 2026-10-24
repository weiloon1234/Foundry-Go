package metadata

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// ValueSlot retains the generated extension API during consumer migration.
// Example: slot.From(manager).
type ValueSlot[M any, K comparable, V any] struct {
	query.ExtensionSlot[M]
	core *valueSlot[M, K, V]
}

func (v ValueSlot[M, K, V]) state() valueSlot[M, K, V] {
	if v.core == nil {
		return valueSlot[M, K, V]{}
	}
	return *v.core
}

// DefineValue preserves the generated declaration boundary. Example: DefineValue(owner, name, spec, inferred, binding).
func DefineValue[M any, K comparable, V any](owner extensions.Owner[M, K], name Name, spec Spec[V], inferred contract.JSON[V], binding extensions.SlotBinding[M, K, Value[V]]) ValueSlot[M, K, V] {
	state := defineValue(owner, name, spec, inferred, binding)
	return ValueSlot[M, K, V]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), &state}
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading and writes then report fault.Missing.
// Example: slot.From(manager).
func (v ValueSlot[M, K, V]) From(m *Manager) ValueSlot[M, K, V] {
	state := v.state().From(m)
	return ValueSlot[M, K, V]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), &state}
}

// Key returns the underlying declaration for the explicit metadata API.
// Example: slot.Key().
func (v ValueSlot[M, K, V]) Key() Key[M, K, V] { return v.state().Key() }

// Name is the stored key name.
// Example: slot.Name().
func (v ValueSlot[M, K, V]) Name() Name { return v.state().Name() }

// Describe returns the slot's inspection snapshot.
// Example: slot.Describe().
func (v ValueSlot[M, K, V]) Describe() extensions.SlotDescription { return v.state().Describe() }

// Validate checks the declaration, contract and generated binding.
// Example: slot.Validate().
func (v ValueSlot[M, K, V]) Validate() error { return v.state().Validate() }

// Registration is the manager registration of the underlying key, including
// the slot's contract and binding validation.
// Example: slot.Registration().
func (v ValueSlot[M, K, V]) Registration() Registration { return v.state().Registration() }

// SaveIn stores input for owner inside tx with the declared version and
// contract, replacing any previous value. A parent rollback rolls it back.
// Example: slot.SaveIn(ctx, tx, owner, input).
func (v ValueSlot[M, K, V]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input V) error {
	return v.state().SaveIn(ctx, tx, owner, input)
}

// Save is SaveIn in the manager's own transaction.
// Example: slot.Save(ctx, owner, input).
func (v ValueSlot[M, K, V]) Save(ctx context.Context, owner M, input V) error {
	return v.state().Save(ctx, owner, input)
}

// ForgetIn removes owner's value inside tx and reports whether one existed.
// Example: slot.ForgetIn(ctx, tx, owner).
func (v ValueSlot[M, K, V]) ForgetIn(ctx context.Context, tx *database.Tx, owner M) (bool, error) {
	return v.state().ForgetIn(ctx, tx, owner)
}

// Forget is ForgetIn in the manager's own transaction.
// Example: slot.Forget(ctx, owner).
func (v ValueSlot[M, K, V]) Forget(ctx context.Context, owner M) (bool, error) {
	return v.state().Forget(ctx, owner)
}

// Format delegates to the shared slot state. Example: slot.Format(s, verb).
func (v ValueSlot[M, K, V]) Format(s fmt.State, verb rune) { v.state().Format(s, verb) }
