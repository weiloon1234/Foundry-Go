package querycore

import (
	"context"
	"database/sql/driver"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldMutator is model-independent transform metadata shared by both APIs.
// Example: NewFieldMutator("email", codec.String[string](), normalize).
type FieldMutator struct {
	Column        string
	Input, Output reflect.Type
	Apply         func(AssignmentValue) (AssignmentValue, error)
}

// NewFieldMutator binds typed input and output once, without reflective invocation.
// Example: NewFieldMutator("email", codec.String[string](), normalize).
func NewFieldMutator[I, V any](column string, c codec.Codec[V], mutate func(I) (V, error)) FieldMutator {
	return NewCapturedFieldMutator(column, func(v V) AssignmentValue { return CaptureAssignment(c, v) }, mutate)
}

// NewCapturedFieldMutator reuses a declared column's typed stored-value capture.
// Example: NewCapturedFieldMutator("id", captureID, convertFreshID).
func NewCapturedFieldMutator[I, V any](column string, capture func(V) AssignmentValue, mutate func(I) (V, error)) FieldMutator {
	return FieldMutator{Column: column, Input: reflect.TypeFor[I](), Output: reflect.TypeFor[V](), Apply: func(a AssignmentValue) (AssignmentValue, error) {
		input, ok := a.InputValue().(I)
		if !ok {
			return AssignmentValue{}, fault.New(fault.Invalid, "mutation value does not match its declared field type")
		}
		output, err := mutate(input)
		if err != nil {
			return AssignmentValue{}, lifecore.Failure("mutate model field "+column+" failed", err)
		}
		return capture(output), nil
	}}
}

// NewNullableFieldMutator retains NULL while transforming assigned non-null inputs.
// Example: NewNullableFieldMutator("note", codec.String[string](), normalize).
func NewNullableFieldMutator[I, V any](column string, c codec.Codec[V], mutate func(I) (V, error)) FieldMutator {
	return NewFieldMutator(column, codec.Nullable(c), func(input value.Nullable[I]) (value.Nullable[V], error) {
		v, ok := input.Get()
		if !ok {
			return value.Null[V](), nil
		}
		out, err := mutate(v)
		if err != nil {
			return value.Nullable[V]{}, err
		}
		return value.Of(out), nil
	})
}

// CaptureAssignment owns a typed stored value and its codec snapshot.
// Example: CaptureAssignment(codec.String[string](), "stored").
func CaptureAssignment[V any](c codec.Codec[V], v V) AssignmentValue {
	v = c.Clone(v)
	return AssignmentValue{Value: v, CopyValue: func() any { return c.Clone(v) }, Bind: func() (driver.Value, error) { return c.Bind(c.Clone(v)) }}
}

// CaptureInput defers binding until transformation, cloning mutable inputs when declared.
// Example: CaptureInput(input, cloneInput).
func CaptureInput[I any](input I, clone func(I) I) AssignmentValue {
	if clone != nil {
		input = clone(input)
	}
	a := AssignmentValue{Value: input, Bind: func() (driver.Value, error) {
		return nil, fault.New(fault.Invalid, "mutation input must be transformed before SQL binding")
	}}
	if clone != nil {
		a.CopyValue = func() any { return clone(input) }
	}
	return a
}

// MutateAssignments checks all input types before invoking transforms in field order.
// It copies assignments so reusing a caller's write starts from the original inputs.
// Example: MutateAssignments(ctx, assignments, fields).
func MutateAssignments(ctx context.Context, assignments []MutationAssignment, fields []FieldMutator) ([]MutationAssignment, error) {
	result := slices.Clone(assignments)
	indices := make(map[string]int, len(result))
	for i, a := range result {
		indices[a.Field.Column] = i
	}
	for _, f := range fields {
		if i, set := indices[f.Column]; set && reflect.TypeOf(result[i].Value) != f.Input {
			return nil, fault.New(fault.Invalid, "mutation value does not match its declared field type")
		}
	}
	for _, f := range fields {
		i, set := indices[f.Column]
		if !set {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a, err := f.Apply(result[i].AssignmentValue)
		if err != nil {
			return nil, err
		}
		result[i].AssignmentValue = a
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
