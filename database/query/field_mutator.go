package query

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// NewMutatedModelField is a generated declaration boundary for same-type
// transformations. Mutators are pure, receive no hydrated model or connection,
// and must not perform I/O. A nil mutator invalidates the definition.
func NewMutatedModelField[M, V any](column string, c codec.Codec[V], get func(M) V, mutate func(V) (V, error)) ModelField[M] {
	return NewInputModelField[M, V, V](column, c, get, mutate)
}

// NewInputModelField is a generated declaration boundary. V owns stored
// extraction and codec validation; I owns the typed mutation input. I need not
// have a database codec. Stored values cannot substitute for a distinct I.
func NewInputModelField[M, V, I any](column string, c codec.Codec[V], get func(M) V, mutate func(I) (V, error)) ModelField[M] {
	if mutate == nil {
		return ModelField[M]{}
	}
	f := NewModelField(column, c, get)
	f.mutator = newFieldMutator(column, c, mutate)
	f.conflictMutator = f.mutator
	return f
}

// Transformations own field values, not model/query scopes. Keeping this
// private callback independent of M prevents write descriptors from expanding
// the runtime type graph of every read, alias and projection field.
type fieldMutator struct {
	input  reflect.Type
	output reflect.Type
	apply  func(assignmentValue) (assignmentValue, error)
}

func newFieldMutator[I, V any](column string, c codec.Codec[V], mutate func(I) (V, error)) fieldMutator {
	return fieldMutator{input: reflect.TypeFor[I](), output: reflect.TypeFor[V](), apply: func(a assignmentValue) (assignmentValue, error) {
		input, ok := a.inputValue().(I)
		if !ok {
			return assignmentValue{}, fault.New(fault.Invalid, "mutation value does not match its declared field type")
		}
		output, err := mutate(input)
		if err != nil {
			return assignmentValue{}, fmt.Errorf("mutate model field %s: %w", column, err)
		}
		return captureAssignment(c, output), nil
	}}
}

// NewNullableMutatedModelField preserves explicit NULL and omission while
// transforming assigned non-null values of the stored field's own type.
func NewNullableMutatedModelField[M, V any](column string, c codec.Codec[V], get func(M) value.Nullable[V], mutate func(V) (V, error)) ModelField[M] {
	return NewNullableInputModelField[M, V, V](column, c, get, mutate)
}

// NewNullableInputModelField converts assigned non-null inputs while retaining
// NULL as NULL. Drafts use Nullable[I]; models and their codec use Nullable[V].
func NewNullableInputModelField[M, V, I any](column string, c codec.Codec[V], get func(M) value.Nullable[V], mutate func(I) (V, error)) ModelField[M] {
	if mutate == nil {
		return ModelField[M]{}
	}
	f := NewInputModelField(column, codec.Nullable(c), get, func(input value.Nullable[I]) (value.Nullable[V], error) {
		v, present := input.Get()
		if !present {
			return value.Null[V](), nil
		}
		output, err := mutate(v)
		if err != nil {
			return value.Nullable[V]{}, err
		}
		return value.Of(output), nil
	})
	// Nullable field Set accepts the same non-null I as its scalar input.
	// SetNull is a distinct conflict mode and never invokes a scalar mutator.
	f.conflictMutator = newFieldMutator(column, c, mutate)
	return f
}

func (q Query[M]) hasFieldMutators() bool {
	if q.definition != nil {
		for _, field := range q.definition.modelFields {
			if field.mutator.apply != nil {
				return true
			}
		}
	}
	return false
}

// mutateFields owns transformation, never compilation or codec binding. It
// copies assignments so reusing the caller's draft/mutation starts from its
// original inputs. Definition order makes invocation independent of setter order.
func (q Query[M]) mutateFields(ctx context.Context, mutation Mutation[M]) (Mutation[M], error) {
	return q.mutateAssignments(ctx, mutation, false)
}

func (q Query[M]) mutateAssignments(ctx context.Context, mutation Mutation[M], conflict bool) (Mutation[M], error) {
	result := Change(mutation.assignments...)
	indices := make(map[string]int, len(result.assignments))
	for i, assignment := range result.assignments {
		indices[assignment.field.column] = i
	}
	// Check all participating value types before invoking any user code.
	for _, field := range q.definition.modelFields {
		mutator := field.mutatorFor(conflict)
		if i, set := indices[field.column]; set && mutator.apply != nil && reflect.TypeOf(result.assignments[i].value) != mutator.input {
			return Mutation[M]{}, fault.New(fault.Invalid, "mutation value does not match its declared field type")
		}
	}
	for _, field := range q.definition.modelFields {
		mutator := field.mutatorFor(conflict)
		if mutator.apply == nil {
			continue
		}
		i, set := indices[field.column]
		if !set {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Mutation[M]{}, err
		}
		assignment, err := mutator.apply(result.assignments[i].assignmentValue)
		if err != nil {
			return Mutation[M]{}, err
		}
		result.assignments[i].assignmentValue = assignment
	}
	if err := ctx.Err(); err != nil {
		return Mutation[M]{}, err
	}
	return result, nil
}

func (f RecordField[M]) mutatorFor(conflict bool) fieldMutator {
	if conflict {
		return f.conflictMutator
	}
	return f.mutator
}
