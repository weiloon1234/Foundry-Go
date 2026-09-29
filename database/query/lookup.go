package query

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// ValueLookup retains a model's filtered source and one concrete stored field.
// It is immutable and never hydrates models or invokes retrieval hooks. It is
// an observation, not a reservation or authorization grant.
type ValueLookup[M any, V comparable] struct {
	source Query[M]
	field  ScalarField[M, V]
	err    error
}

// Lookup binds a generated field to a generated model query. Filters and soft
// deletion visibility are preserved. Pagination, ordering and eager loading
// are rejected: a lookup examines the complete matching scope. Nullable fields
// accept their non-null value type; nullable input is handled by the caller.
func Lookup[M any, V comparable](source ModelQuerySource[M], field KeyField[M, V]) ValueLookup[M, V] {
	if nilDescriptor(source) || nilDescriptor(field) {
		return ValueLookup[M, V]{err: fault.New(fault.Invalid, "model lookup requires a source and field")}
	}
	return ValueLookup[M, V]{source: source.modelQuery(), field: field.relationKey()}
}

// Validate checks the query and field declarations without binding a received
// value, executing a codec or accessing the database.
func (l ValueLookup[M, V]) Validate() error {
	if l.err != nil {
		return l.err
	}
	q := l.source
	if q.definition == nil || q.limit.IsSet() || q.offset != 0 || len(q.orders) != 0 || len(q.relations) != 0 || q.relationLimits != nil {
		return fault.New(fault.Invalid, "model lookup requires an unpaginated model scope without ordering or eager loading")
	}
	var zero V
	if err := q.Where(l.field.Eq(zero)).Validate(); err != nil {
		return err
	}
	if _, ok := q.definition.modelField(l.field.ref.column); !ok {
		return fault.New(fault.Invalid, "model lookup field is not a stored model field")
	}
	return nil
}

// Exists reuses the model query compiler and executor with a parameterized
// equality predicate. It opens no transaction and runs no model observers.
func (l ValueLookup[M, V]) Exists(ctx context.Context, executor database.Executor, input V) (bool, error) {
	if err := l.Validate(); err != nil {
		return false, err
	}
	return l.source.Where(l.field.Eq(input)).Exists(ctx, executor)
}

// AllExist checks at most 64 equality observations per statement. It uses the
// database's equality and field codec for every value, including values with
// different Go representations that compare equal in SQL. No count of raw Go
// keys is compared with COUNT(DISTINCT ...). Empty input performs no I/O.
// The complete operation is advisory; separate batches may observe changes.
func (l ValueLookup[M, V]) AllExist(ctx context.Context, executor database.Executor, input []V) (bool, error) {
	if err := l.Validate(); err != nil {
		return false, err
	}
	if ctx == nil {
		return false, fault.New(fault.Invalid, "model lookup requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	const batchSize = 64
	for start := 0; start < len(input); start += batchSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		end := min(start+batchSize, len(input))
		predicates := make([]Predicate[M], 0, end-start)
		for _, item := range input[start:end] {
			predicates = append(predicates, ExistsQuery(l.source, l.source.Where(l.field.Eq(item))))
		}
		found, err := l.source.Where(predicates...).Exists(ctx, executor)
		if err != nil || !found {
			return false, err
		}
	}
	return true, nil
}

// AnyExist reports whether any value matches, with one parameterized IN
// predicate of at most 64 values per statement and the field's SQL equality.
// It stops at the first matching batch. Empty input performs no I/O.
func (l ValueLookup[M, V]) AnyExist(ctx context.Context, executor database.Executor, input []V) (bool, error) {
	if err := l.Validate(); err != nil {
		return false, err
	}
	if ctx == nil {
		return false, fault.New(fault.Invalid, "model lookup requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	const batchSize = 64
	for start := 0; start < len(input); start += batchSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		found, err := l.source.Where(l.field.In(input[start:min(start+batchSize, len(input))]...)).Exists(ctx, executor)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func (ValueLookup[M, V]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model value lookup"))
}
