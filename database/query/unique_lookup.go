package query

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// UniqueLookup hydrates a model by one stored key inside an immutable query
// scope. The key must be unique in that scope; a second match is an internal
// data/configuration error. A database unique constraint remains recommended.
type UniqueLookup[M any, K comparable] struct {
	source Query[M]
	field  ScalarField[M, K]
	err    error
}

// Unique retains filters, ordering, soft-delete visibility, retrieval hooks and
// eager loads. Pagination is rejected because it could conceal duplicate keys.
// Unlike Lookup (an existence observation), Unique hydrates the complete model.
func Unique[M any, K comparable](source ModelQuerySource[M], field KeyField[M, K]) UniqueLookup[M, K] {
	if nilDescriptor(source) || nilDescriptor(field) {
		return UniqueLookup[M, K]{err: fault.New(fault.Invalid, "unique lookup requires a model source and stored field")}
	}
	return UniqueLookup[M, K]{source: source.modelQuery(), field: field.relationKey()}
}
func (l UniqueLookup[M, K]) Validate() error {
	if l.err != nil {
		return l.err
	}
	q := l.source
	if q.definition == nil || q.limit.IsSet() || q.offset != 0 {
		return fault.New(fault.Invalid, "unique lookup requires an unpaginated model scope")
	}
	var zero K
	if err := q.Where(l.field.Eq(zero)).Validate(); err != nil {
		return err
	}
	if _, ok := q.definition.modelField(l.field.ref.column); !ok {
		return fault.New(fault.Invalid, "unique lookup field is not stored on its model")
	}
	return nil
}

// Find uses one parameterized SELECT bounded to two rows. There is no count/find
// race, fallback query, implicit transaction or lock. Eager loads run only for a
// unique result; retrieval callbacks keep their ordinary query behavior.
func (l UniqueLookup[M, K]) Find(ctx context.Context, executor database.Executor, key K) (value.Optional[M], error) {
	if err := l.Validate(); err != nil {
		return value.Optional[M]{}, err
	}
	return uniqueModel(ctx, executor, l.source.Where(l.field.Eq(key)))
}
func uniqueModel[M any](ctx context.Context, executor database.Executor, q Query[M]) (value.Optional[M], error) {
	rows, err := q.Limit(2).allRows(ctx, executor)
	if err != nil {
		return value.Optional[M]{}, err
	}
	if len(rows) > 1 {
		return value.Optional[M]{}, fault.New(fault.Internal, "model lookup key has multiple matches in its scope")
	}
	if len(rows) == 0 {
		return value.Optional[M]{}, nil
	}
	if len(q.relations) != 0 {
		rows, err = q.Load(ctx, executor, rows)
		if err != nil {
			return value.Optional[M]{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return value.Optional[M]{}, err
	}
	return value.Set(rows[0]), nil
}

// Format omits scoped values and internal model metadata from diagnostics.
func (UniqueLookup[M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("unique model lookup"))
}
