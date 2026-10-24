package metadata

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

// ValueHandle retains typed slot capabilities behind shared callback state.
// Example: slot.From(manager).Relation(Fields.Slot).
type ValueHandle[M any, K comparable, V any] struct {
	_    [0]*M
	_    [0]*K
	_    [0]*V
	core *valueHandleCore
}

// Callback pointers are created and consumed together here; they never cross the public API.
// Calls are synchronous: no callback retains a pointer to an argument or result.
type valueHandleCore struct {
	bind         func(*Manager) *valueHandleCore
	relation     func(unsafe.Pointer)
	validate     func() error
	describe     func() extensions.SlotDescription
	registration func() Registration
	name         func() Name
	save         func(ctx context.Context, owner unsafe.Pointer, input unsafe.Pointer) error
	saveIn       func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input unsafe.Pointer) error
	forget       func(ctx context.Context, owner unsafe.Pointer) (bool, error)
	forgetIn     func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer) (bool, error)
}

// ValueOf declares a thin metadata handle retaining the concrete input and JSON contract.
// Example: metadata.ValueOf(owner,"seo",spec,contract.StringJSON[string](),binding).
func ValueOf[M any, K comparable, V any](owner extensions.Owner[M, K], name Name, spec Spec[V], inferred contract.JSON[V], binding extensions.SlotBinding[M, K, Value[V]]) ValueHandle[M, K, V] {
	return ValueHandle[M, K, V]{core: wrapValueHandle(DefineValue(owner, name, spec, inferred, binding))}
}

func wrapValueHandle[M any, K comparable, V any](legacy ValueSlot[M, K, V]) *valueHandleCore {
	return &valueHandleCore{
		bind:         func(m *Manager) *valueHandleCore { return wrapValueHandle(legacy.From(m)) },
		relation:     func(out unsafe.Pointer) { *(*query.ExtensionSlot[M])(out) = legacy.ExtensionSlot },
		validate:     legacy.Validate,
		describe:     legacy.Describe,
		registration: legacy.Registration,
		name:         legacy.Name,
		save: func(ctx context.Context, owner unsafe.Pointer, input unsafe.Pointer) error {
			return legacy.Save(ctx, *(*M)(owner), *(*V)(input))
		},
		saveIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input unsafe.Pointer) error {
			return legacy.SaveIn(ctx, tx, *(*M)(owner), *(*V)(input))
		},
		forget: func(ctx context.Context, owner unsafe.Pointer) (bool, error) { return legacy.Forget(ctx, *(*M)(owner)) },
		forgetIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer) (bool, error) {
			return legacy.ForgetIn(ctx, tx, *(*M)(owner))
		},
	}
}

// From returns a copy borrowing this family's manager; nil leaves it unbound.
// Example: slot.From(manager).
func (s ValueHandle[M, K, V]) From(m *Manager) ValueHandle[M, K, V] {
	if s.core != nil {
		s.core = s.core.bind(m)
	}
	return s
}

// Relation binds the loader to its concrete ORM field; mismatched fields fail validation.
// Example: Articles.Query().With(slot.Relation(Articles.SEO)).
func (s ValueHandle[M, K, V]) Relation(slot orm.Slot[M, Value[V]]) orm.ExtensionRelation[M] {
	var source query.ExtensionSlot[M]
	if s.core != nil {
		s.core.relation(unsafe.Pointer(&source))
	}
	return orm.Extension(slot, source)
}

// Validate checks the declaration and slot binding. Example: slot.Validate().
func (s ValueHandle[M, K, V]) Validate() error {
	if s.core == nil {
		return invalid()
	}
	return s.core.validate()
}

// Describe returns an owned inspection snapshot. Example: slot.Describe().
func (s ValueHandle[M, K, V]) Describe() extensions.SlotDescription {
	if s.core == nil {
		return *new(extensions.SlotDescription)
	}
	return s.core.describe()
}

// Registration returns the existing manager registration. Example: slot.Registration().
func (s ValueHandle[M, K, V]) Registration() Registration {
	if s.core == nil {
		return *new(Registration)
	}
	return s.core.registration()
}

// Name returns the stored name. Example: slot.Name().
func (s ValueHandle[M, K, V]) Name() Name {
	if s.core == nil {
		return *new(Name)
	}
	return s.core.name()
}

// Save replaces the typed value in the manager transaction.
// Example: slot.Save(ctx,article,input).
func (s ValueHandle[M, K, V]) Save(ctx context.Context, owner M, input V) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.save(ctx, unsafe.Pointer(&owner), unsafe.Pointer(&input))
}

// SaveIn replaces the typed value in the caller transaction.
// Example: slot.SaveIn(ctx,tx,article,input).
func (s ValueHandle[M, K, V]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input V) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.saveIn(ctx, tx, unsafe.Pointer(&owner), unsafe.Pointer(&input))
}

// Forget removes the stored value and reports whether it existed.
// Example: slot.Forget(ctx,article).
func (s ValueHandle[M, K, V]) Forget(ctx context.Context, owner M) (bool, error) {
	if s.core == nil {
		return *new(bool), invalid()
	}
	return s.core.forget(ctx, unsafe.Pointer(&owner))
}

// ForgetIn removes the stored value in the caller transaction.
// Example: slot.ForgetIn(ctx,tx,article).
func (s ValueHandle[M, K, V]) ForgetIn(ctx context.Context, tx *database.Tx, owner M) (bool, error) {
	if s.core == nil {
		return *new(bool), invalid()
	}
	return s.core.forgetIn(ctx, tx, unsafe.Pointer(&owner))
}

// Format omits retained runtime state. Example: fmt.Sprintf("%+v",slot).
func (ValueHandle[M, K, V]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("metadata slot handle"))
}
