package orm

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// InputKey retains stored K for lookup and accepts I for primary-key assignment.
// Example: InputKey[User, FreshID, model.ID[User]].
type InputKey[M, I any, K comparable] = ColumnHandle[M, K, I, K, KeyKind, InputKeyRole]

// InputKeyOf adds a typed input to the primary key already declared by TableOf.
// A nil transform selects the matching Mutate<Field> method; distinct inputs disable automatic UUID creation.
// Call inside Define. Example: InputKeyOf[User, FreshID](b, table.Key(), nil).
func InputKeyOf[M, I any, K comparable](b Builder[M], key Key[M, K], transform func(I) (K, error)) InputKey[M, I, K] {
	d, core := b.state, columnCoreOf(key.stored)
	if d == nil || d.sealed {
		return InputKey[M, I, K]{stored: &columnData[K]{core: &columnCore{owner: &tableCore{err: invalid("InputKeyOf must run inside Define")}}}}
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
		core.mutator = querycore.NewCapturedFieldMutator(core.metadata.Name, key.stored.assign, typed)
		return nil
	}
	bindNamedMutator[M, I, K](core)
	if transform != nil {
		core.explicitMutator = true
		_ = core.bindMutator(transform)
	}
	clone := ormcore.InputClone[I]()
	return InputKey[M, I, K]{stored: key.stored, input: func(v I) querycore.AssignmentValue {
		return querycore.CaptureInput(v, clone)
	}}
}
