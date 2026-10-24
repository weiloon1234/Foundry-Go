package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
)

// InputKey retains stored K for lookup and accepts I for primary-key assignment.
// Example: InputKey[User, FreshID, model.ID[User]].
type InputKey[M, I any, K comparable] struct {
	_     [0]*M
	key   columnData[K]
	input func(I) querycore.AssignmentValue
}

// InputKeyOf adds a typed input to the primary key already declared by TableOf.
// A nil transform selects the matching Mutate<Field> method; distinct inputs disable automatic UUID creation.
// Call inside Define. Example: InputKeyOf[User, FreshID](b, table.Key(), nil).
func InputKeyOf[M, I any, K comparable](b Builder[M], key Key[M, K], transform func(I) (K, error)) InputKey[M, I, K] {
	d, core := b.state, key.key.core
	if d == nil || d.sealed {
		return InputKey[M, I, K]{key: columnData[K]{core: &columnCore{owner: &tableCore{err: invalid("InputKeyOf must run inside Define")}}}}
	}
	if core == nil || core.owner != d.core || core.metadata.Name != d.core.model.Primary {
		d.fail("primary", "InputKeyOf requires this declaration's existing primary key")
		return InputKey[M, I, K]{}
	}
	if core.primaryMapping || core.requiresMutator || core.explicitMutator {
		d.fail(core.field.Name, "primary input is already configured")
		return InputKey[M, I, K]{}
	}
	core.requiresMutator = true
	checkInputType(d, core, reflect.TypeFor[I]())
	core.bindMutator = func(fn any) error {
		typed, ok := fn.(func(I) (K, error))
		if !ok {
			return invalid("primary mutator signature does not match its declared input and stored key")
		}
		core.mutator = querycore.NewCapturedFieldMutator(core.metadata.Name, key.key.assign, typed)
		return nil
	}
	bindNamedMutator[M, I, K](core)
	if transform != nil {
		core.explicitMutator = true
		_ = core.bindMutator(transform)
	}
	clone := ormcore.InputClone[I]()
	return InputKey[M, I, K]{key: key.key, input: func(v I) querycore.AssignmentValue { return querycore.CaptureInput(v, clone) }}
}

// Eq compares the stored key. Example: users.ID.Eq(storedID).
func (c InputKey[M, I, K]) Eq(v K) Predicate[M] { return comparison[M](c.key, querycore.Equal, []K{v}) }

// Ne excludes a stored key. Example: users.ID.Ne(storedID).
func (c InputKey[M, I, K]) Ne(v K) Predicate[M] {
	return comparison[M](c.key, querycore.NotEqual, []K{v})
}

// In matches stored keys. Example: users.ID.In(firstID, secondID).
func (c InputKey[M, I, K]) In(v ...K) Predicate[M] { return comparison[M](c.key, querycore.In, v) }

// NotIn excludes stored keys. Example: users.ID.NotIn(firstID, secondID).
func (c InputKey[M, I, K]) NotIn(v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.NotIn, v)
}

// Asc orders by stored key. Example: users.ID.Asc().
func (c InputKey[M, I, K]) Asc() Order[M] { return columnOrder[M](c.key, false) }

// Desc orders by stored key. Example: users.ID.Desc().
func (c InputKey[M, I, K]) Desc() Order[M] { return columnOrder[M](c.key, true) }

// Set captures fresh input. Example: users.ID.Set(FreshID{Value: storedID}).
func (c InputKey[M, I, K]) Set(v I) Assignment[M]      { return inputAssignment[M](c.key.core, c.input, v) }
func (c InputKey[M, I, K]) ormColumn(*M) columnData[K] { return c.key }
func (c InputKey[M, I, K]) expressionColumn(*M) (ormcore.Column[K], ormcore.Column[K]) {
	return requiredExpression(c.key)
}
