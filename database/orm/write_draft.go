package orm

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Write is an operation-local draft. Use it only during Saving/Creating/Updating.
// Example: WriteSet(write, orm.NumberSet(Orders.TotalCents, 0)).
type Write[M any] struct {
	_     [0]*M
	state *draftState
}
type draftState struct {
	table       *tableCore
	assignments []querycore.MutationAssignment
	err         error
	open        bool
}

// String redacts pending input. Example: fmt.Sprint(write).
func (*Write[M]) String() string { return "orm write draft" }

// GoString redacts pending input. Example: fmt.Sprintf("%#v",write).
func (*Write[M]) GoString() string { return "orm write draft" }

// WriteSet replaces assigned fields; omission remains distinct from zero and NULL.
// Example: WriteSet(write, InputTextSet(Members.Name, ""), NullableInputTextSetNull(Members.Note)).
func WriteSet[M any](write *Write[M], assignments ...Assignment[M]) error {
	if write == nil || write.state == nil || !write.state.open {
		return invalid("write draft is outside its callback")
	}
	d := write.state
	for _, a := range assignments {
		if a.err != nil {
			d.err = a.err
			return d.err
		}
		if a.owner != d.table || a.adjust != nil {
			d.err = invalid("write draft requires a matching field assignment")
			return d.err
		}

		replaced := false
		for i, old := range d.assignments {
			if old.Field.Column == a.core.Field.Column {
				d.assignments[i] = a.core
				replaced = true
				break
			}
		}
		if !replaced {
			d.assignments = append(d.assignments, a.core)
		}
	}
	return nil
}

// WriteGet reads a required field's input without running its mutator.
// Example: input, supplied := WriteGet(write, func(v EmailInput) Assignment[Member] { return InputTextSet(Members.Email, v) }).Get().
func WriteGet[M, I any](write *Write[M], setter func(I) Assignment[M]) value.Optional[I] {
	a, ok := draftAssignment(write, setter)
	if !ok {
		return value.Optional[I]{}
	}
	raw := a.InputValue()
	input, ok := raw.(I)
	if !ok {
		write.state.err = invalid("required draft getter has a different input type; use WriteNullableGet for nullable fields")
		return value.Optional[I]{}
	}
	return value.Set(input)
}

// WriteNullableGet preserves omission, SQL NULL and the distinct mutation input.
// Example: input := WriteNullableGet(write, func(v LabelInput) Assignment[Member] { return NullableInputTextSet(Members.Note, v) }).
func WriteNullableGet[M, I any](write *Write[M], setter func(I) Assignment[M]) value.Optional[value.Nullable[I]] {
	a, ok := draftAssignment(write, setter)
	if !ok {
		return value.Optional[value.Nullable[I]]{}
	}
	raw := a.InputValue()
	input, ok := raw.(value.Nullable[I])
	if !ok {
		write.state.err = invalid("nullable draft getter has a different input type")
		return value.Optional[value.Nullable[I]]{}
	}
	return value.Set(input)
}

// WriteIsSet reports assignment independently of input value or NULL.
// Example: WriteIsSet(write, func(v LabelInput) Assignment[Member] { return NullableInputTextSet(Members.Note, v) }).
func WriteIsSet[M, I any](write *Write[M], setter func(I) Assignment[M]) bool {
	_, ok := draftAssignment(write, setter)
	return ok
}
func draftAssignment[M, I any](write *Write[M], setter func(I) Assignment[M]) (querycore.AssignmentValue, bool) {
	column, err := draftField(write, setter)
	if err != nil {
		if write != nil && write.state != nil {
			write.state.err = err
		}
		return querycore.AssignmentValue{}, false
	}
	for _, a := range write.state.assignments {
		if a.Field == column {
			return a.AssignmentValue, true
		}
	}
	return querycore.AssignmentValue{}, false
}

func draftField[M, I any](write *Write[M], setter func(I) Assignment[M]) (querycore.FieldRef, error) {
	if write == nil || write.state == nil || !write.state.open || setter == nil {
		return querycore.FieldRef{}, invalid("write draft is outside its callback or has no field setter")
	}
	var zero I
	column := setter(zero)
	if column.err != nil || column.owner != write.state.table || column.adjust != nil {
		return querycore.FieldRef{}, invalid("draft access requires a matching field setter")
	}
	return column.core.Field, nil
}

// WriteUnset removes a pending assignment; the field becomes omitted, not NULL or zero.
// Unsetting an omitted field is a no-op. Later hooks may assign it again, and
// conventions may supply an automatic value. Use only during a before-write callback.
// Example: WriteUnset(write, func(v EmailInput) Assignment[Member] { return InputTextSet(Members.Email, v) }).
func WriteUnset[M, I any](write *Write[M], setter func(I) Assignment[M]) error {
	field, err := draftField(write, setter)
	if err != nil {
		if write != nil && write.state != nil {
			write.state.err = err
		}
		return err
	}
	d := write.state
	d.assignments = slices.DeleteFunc(d.assignments, func(a querycore.MutationAssignment) bool { return a.Field == field })
	return d.err
}

// WithHooks installs a factory invoked once per normal write, after row locking.
// Example: Define("orders", declareOrders, WithHooks(func() Hooks[Order] { return Hooks[Order]{} })).
func WithHooks[M any](factory func() Hooks[M]) Option {
	var stored func() hookSet
	if factory != nil {
		stored = func() hookSet { return eraseHooks(factory()) }
	}
	return Option{hooks: stored, hookModel: reflect.TypeFor[M]()}
}
