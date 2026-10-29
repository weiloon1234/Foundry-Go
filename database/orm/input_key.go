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

// InputKeyEq compares the stored key. Example: InputKeyEq(users.ID, storedID).
func InputKeyEq[M, I any, K comparable](c InputKey[M, I, K], v K) Predicate[M] {
	return comparison[M](c.key, querycore.Equal, []K{v})
}

// InputKeyNe excludes a stored key. Example: InputKeyNe(users.ID, storedID).
func InputKeyNe[M, I any, K comparable](c InputKey[M, I, K], v K) Predicate[M] {
	return comparison[M](c.key, querycore.NotEqual, []K{v})
}

// InputKeyIn matches stored keys. Example: InputKeyIn(users.ID, firstID, secondID).
func InputKeyIn[M, I any, K comparable](c InputKey[M, I, K], v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.In, v)
}

// InputKeyNotIn excludes stored keys. Example: InputKeyNotIn(users.ID, firstID, secondID).
func InputKeyNotIn[M, I any, K comparable](c InputKey[M, I, K], v ...K) Predicate[M] {
	return comparison[M](c.key, querycore.NotIn, v)
}

// InputKeyAsc orders by stored key. Example: InputKeyAsc(users.ID).
func InputKeyAsc[M, I any, K comparable](c InputKey[M, I, K]) Order[M] {
	return columnOrder[M](c.key, false)
}

// InputKeyDesc orders by stored key. Example: InputKeyDesc(users.ID).
func InputKeyDesc[M, I any, K comparable](c InputKey[M, I, K]) Order[M] {
	return columnOrder[M](c.key, true)
}

// InputKeySet captures fresh input. Example: InputKeySet(users.ID, FreshID{Value: storedID}).
func InputKeySet[M, I any, K comparable](c InputKey[M, I, K], v I) Assignment[M] {
	return inputAssignment[M](c.key.core, c.input, v)
}
