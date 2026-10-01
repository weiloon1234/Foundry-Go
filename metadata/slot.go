package metadata

import (
	"context"
	"errors"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Spec declares a metadata slot's stored version and JSON contract. A zero
// Version selects 1 and a zero JSON selects the contract generation inferred
// from the slot's value type (a generated DTO, scalar or json.RawMessage).
// Increment Version for incompatible changes: strict decoding rejects stored
// values with removed or renamed fields.
type Spec[V any] struct {
	Version Version
	JSON    contract.JSON[V]
}

// Value is a model slot holding one typed metadata value. The zero value is
// not loaded; a loaded slot distinguishes an absent key from a stored value.
// Reading a slot performs no I/O. Maps and slices inside a decoded value keep
// ordinary Go value-copy semantics.
type Value[V any] struct {
	value  V
	stored bool
	loaded bool
}

// IsLoaded reports whether the slot was filled by a loader.
func (v Value[V]) IsLoaded() bool { return v.loaded }

// Get returns the optional stored value and whether the slot was loaded.
func (v Value[V]) Get() (value.Optional[V], bool) {
	if !v.stored {
		return value.Optional[V]{}, v.loaded
	}
	return value.Set(v.value), v.loaded
}

func (Value[V]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("metadata value slot")) }
func (Value[V]) MarshalJSON() ([]byte, error) { return nil, invalid() }

// ValueSlot binds one model's Value field to its typed metadata key.
// Generated <Model>Extensions() constructs it; applications bind a manager once
// with From. A bound slot is an eager-loading relation for With and Load.
type ValueSlot[M any, K comparable, V any] struct {
	query.ExtensionSlot[M]
	key     Key[M, K, V]
	binding extensions.SlotBinding[M, K, Value[V]]
	manager *Manager
	err     error
}

// DefineValue is the generated declaration boundary for a Value slot. The
// inferred contract applies only when spec.JSON is zero; without either the
// slot is invalid and its registration fails at assembly.
func DefineValue[M any, K comparable, V any](owner extensions.Owner[M, K], name Name, spec Spec[V], inferred contract.JSON[V], binding extensions.SlotBinding[M, K, Value[V]]) ValueSlot[M, K, V] {
	if spec.Version == 0 {
		spec.Version = 1
	}
	var err error
	if spec.JSON.IsZero() {
		if inferred.IsZero() {
			err = fault.New(fault.Invalid, "metadata slot "+binding.Field+" requires an explicit JSON contract in DefineExtensions")
		}
		spec.JSON = inferred
	}
	return ValueSlot[M, K, V]{key: Define(owner, name, spec.Version, spec.JSON), binding: binding, err: err}.From(nil)
}

// Key returns the underlying declaration for the explicit metadata API.
func (s ValueSlot[M, K, V]) Key() Key[M, K, V] { return s.key }

// Name is the stored key name.
func (s ValueSlot[M, K, V]) Name() Name { return s.key.Name() }

// Describe returns the slot's inspection snapshot.
func (s ValueSlot[M, K, V]) Describe() extensions.SlotDescription {
	return extensions.SlotDescription{Field: s.binding.Field, Kind: "value", Name: string(s.Name()), Storage: "foundry_model_metadata", MaxBytes: MaxValueBytes, Version: uint32(s.key.Version())}
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading and writes then report fault.Missing.
func (s ValueSlot[M, K, V]) From(m *Manager) ValueSlot[M, K, V] {
	s.manager = m
	binding := query.ExtensionBinding[M, Value[V]]{
		Name: s.binding.Field, Get: s.binding.Get, Set: s.binding.Set,
		Loaded:  Value[V].IsLoaded,
		Count:   func(v Value[V]) int { return boolCount(v.stored) },
		Unbound: s.unbound(),
	}
	if s.key.definition != nil {
		binding.Table = s.key.definition.owner.ModelName()
	}
	if m != nil {
		binding.Fetch = s.fetch
	}
	s.ExtensionSlot = query.NewExtensionSlot(binding)
	return s
}

// Validate checks the declaration, contract and generated binding.
func (s ValueSlot[M, K, V]) Validate() error {
	if s.err != nil {
		return s.err
	}
	if err := s.binding.Validate(); err != nil {
		return err
	}
	return s.key.Validate()
}

// Registration is the manager registration of the underlying key, including
// the slot's contract and binding validation.
func (s ValueSlot[M, K, V]) Registration() Registration {
	registration := s.key.Registration()
	if err := s.Validate(); err != nil && registration.validate != nil {
		registration.validate = func(*extensions.Registry) error { return err }
	}
	return registration
}

// bound returns the manager, or a Missing fault naming an unbound slot.
func (s ValueSlot[M, K, V]) bound() (*Manager, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, s.unbound()
	}
	return s.manager, nil
}
func (s ValueSlot[M, K, V]) unbound() error {
	return fault.New(fault.Missing, "metadata slot "+s.binding.Field+" is not bound to a metadata manager; bind it with <Model>Extensions().From(runtime)")
}

// fetch loads one relation batch of parents, halving it when it exceeds the
// JSON byte budget. Owners that are soft-deleted or no longer exist keep an
// unloaded slot.
func (s ValueSlot[M, K, V]) fetch(ctx context.Context, executor database.Executor, parents []M) ([]Value[V], error) {
	m, err := s.bound()
	if err != nil {
		return nil, err
	}
	if err := s.key.check(m); err != nil {
		return nil, err
	}
	return extensions.LoadInParts(ctx, parents, len(parents), extensionvalue.ErrBatchLimit, func(ctx context.Context, part []M) ([]Value[V], error) {
		references := make([]model.Reference[M, K], len(part))
		for i, parent := range part {
			references[i] = s.binding.Reference(parent)
		}
		var batch Batch[M, K, V]
		if err := m.store.ReadFor(ctx, executor, func(ctx context.Context, tx *database.Tx) error {
			var err error
			batch, err = s.key.loadIn(ctx, tx, m, references)
			return err
		}); err != nil {
			return nil, err
		}
		result := make([]Value[V], len(part))
		for i := range references {
			stored, err := batch.getAt(ctx, i)
			if errors.Is(err, database.NotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			decoded, ok := stored.Get()
			result[i] = Value[V]{value: decoded, stored: ok, loaded: true}
		}
		return result, nil
	})
}

// SaveIn stores input for owner inside tx with the declared version and
// contract, replacing any previous value. A parent rollback rolls it back.
func (s ValueSlot[M, K, V]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input V) error {
	if tx == nil {
		return invalid()
	}
	m, err := s.bound()
	if err != nil {
		return err
	}
	return s.key.SetIn(ctx, tx, m, s.binding.Reference(owner), input)
}

// Save is SaveIn in the manager's own transaction.
func (s ValueSlot[M, K, V]) Save(ctx context.Context, owner M, input V) error {
	m, err := s.bound()
	if err != nil {
		return err
	}
	return s.key.Set(ctx, m, s.binding.Reference(owner), input)
}

// ForgetIn removes owner's value inside tx and reports whether one existed.
func (s ValueSlot[M, K, V]) ForgetIn(ctx context.Context, tx *database.Tx, owner M) (bool, error) {
	if tx == nil {
		return false, invalid()
	}
	m, err := s.bound()
	if err != nil {
		return false, err
	}
	if err := s.key.check(m); err != nil {
		return false, err
	}
	removed := false
	err = m.store.Join(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		removed, err = s.key.forget(ctx, tx, m, s.binding.Reference(owner))
		return err
	})
	return removed, err
}

// Forget is ForgetIn in the manager's own transaction.
func (s ValueSlot[M, K, V]) Forget(ctx context.Context, owner M) (bool, error) {
	m, err := s.bound()
	if err != nil {
		return false, err
	}
	return s.key.Forget(ctx, m, s.binding.Reference(owner))
}

func boolCount(present bool) int {
	if present {
		return 1
	}
	return 0
}

func (ValueSlot[M, K, V]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("metadata value slot descriptor"))
}
