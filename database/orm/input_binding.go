package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

func bindInput[I, V any](d *declaration, ptr *V, c codec.Codec[V], transform func(I) (V, error)) (columnData[V], func(I) querycore.AssignmentValue) {
	stored := bindTyped(d, ptr, c, false)
	core := stored.core
	core.requiresMutator = true
	checkInputType(d, core, reflect.TypeFor[I]())
	core.bindMutator = func(fn any) error {
		typed, ok := fn.(func(I) (V, error))
		if !ok {
			return invalid("mutator signature does not match declared input and stored types")
		}
		core.mutator = querycore.NewFieldMutator(core.metadata.Name, c, typed)
		return nil
	}
	if transform != nil {
		core.explicitMutator = true
		_ = core.bindMutator(transform)
	}
	clone := ormcore.InputClone[I]()
	return stored, func(v I) querycore.AssignmentValue { return querycore.CaptureInput(v, clone) }
}
func bindNullableInput[I, V any](d *declaration, ptr *value.Nullable[V], c codec.Codec[V], transform func(I) (V, error)) (nullableColumnData[V], func(I) querycore.AssignmentValue) {
	stored := bindNullable(d, ptr, c)
	core := stored.base.core
	core.requiresMutator = true
	checkInputType(d, core, reflect.TypeFor[I]())
	core.bindMutator = func(fn any) error {
		typed, ok := fn.(func(I) (V, error))
		if !ok {
			return invalid("mutator signature does not match declared nullable input and stored types")
		}
		core.mutator = querycore.NewNullableFieldMutator(core.metadata.Name, c, typed)
		return nil
	}
	if transform != nil {
		core.explicitMutator = true
		_ = core.bindMutator(transform)
	}
	clone := ormcore.InputClone[I]()
	nullableClone := func(v value.Nullable[I]) value.Nullable[I] {
		x, ok := v.Get()
		if !ok {
			return value.Null[I]()
		}
		if clone != nil {
			x = clone(x)
		}
		return value.Of(x)
	}
	// Explicit NULL has the input Nullable[I] type too, and bypasses scalar transforms.
	automatic := core.automatic
	sameType := reflect.TypeFor[I]() == reflect.TypeFor[V]()
	core.automatic = func(raw any) (querycore.AssignmentValue, error) {
		if raw != nil {
			if sameType {
				return automatic(raw)
			}
			return querycore.AssignmentValue{}, invalid("distinct input fields cannot receive automatic stored values")
		}
		return querycore.CaptureInput(value.Null[I](), nullableClone), nil
	}
	return stored, func(v I) querycore.AssignmentValue { return querycore.CaptureInput(value.Of(v), nullableClone) }
}
func inputAssignment[M, I any](core *columnCore, capture func(I) querycore.AssignmentValue, v I) Assignment[M] {
	if core == nil || core.owner == nil || capture == nil {
		return Assignment[M]{err: invalid("uninitialized input column")}
	}
	return Assignment[M]{owner: core.owner, core: querycore.MutationAssignment{Field: querycore.FieldRef{Table: core.owner.model.Table, Column: core.metadata.Name}, AssignmentValue: capture(v)}}
}
func checkInputType(d *declaration, c *columnCore, t reflect.Type) {
	if d != nil && !d.sealed && !modelmeta.ValidMutatorInput(t) {
		d.fail(c.field.Name, "mutator input must be concrete and non-null")
	}
}
