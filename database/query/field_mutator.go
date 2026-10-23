package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
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

// Both APIs share the typed transform and assignment traversal owner.
type fieldMutator = querycore.FieldMutator

func newFieldMutator[I, V any](column string, c codec.Codec[V], mutate func(I) (V, error)) fieldMutator {
	return querycore.NewFieldMutator(column, c, mutate)
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
	f := NewModelField(column, codec.Nullable(c), get)
	f.mutator = querycore.NewNullableFieldMutator(column, c, mutate)
	// Nullable field Set accepts the same non-null I as its scalar input.
	// SetNull is a distinct conflict mode and never invokes a scalar mutator.
	f.conflictMutator = newFieldMutator(column, c, mutate)
	return f
}

func (q Query[M]) hasFieldMutators() bool {
	if q.definition != nil {
		for _, field := range q.definition.modelFields {
			if field.mutator.Apply != nil {
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
	fields := make([]fieldMutator, 0, len(q.definition.modelFields))
	for _, field := range q.definition.modelFields {
		if transform := recordMutator(field.recordFieldMetadata, conflict); transform.Apply != nil {
			fields = append(fields, transform)
		}
	}
	assignments, err := querycore.MutateAssignments(ctx, mutation.assignments, fields)
	if err != nil {
		return Mutation[M]{}, err
	}
	return Mutation[M]{assignments: assignments}, nil
}

func recordMutator(f recordFieldMetadata, conflict bool) fieldMutator {
	if conflict {
		return f.conflictMutator
	}
	return f.mutator
}
