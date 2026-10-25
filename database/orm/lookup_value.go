package orm

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ValueLookup observes a stored field without hydrating models or running retrieval hooks.
// It is advisory, not a reservation. Example: orm.Lookup(Users.Query(), Users.Email).
type ValueLookup[M any, V comparable] struct {
	_         [0]*M
	valueCore *keyLookupState[V]
}

// Lookup binds a stored column's non-null value type to its complete model scope.
// Ordering, pagination and eager loading are rejected. Example: orm.Lookup(Users.Query(), Users.Nickname).
func Lookup[M any, V comparable](source Query[M], field KeyColumn[M, V]) ValueLookup[M, V] {
	if !lookupColumn(field) {
		return ValueLookup[M, V]{valueCore: &keyLookupState[V]{err: invalid("model lookup requires a source and field")}}
	}
	return ValueLookup[M, V]{valueCore: &keyLookupState[V]{state: source.state, field: field.ormColumn(nil)}}
}

// Validate checks declarations without encoding values or performing I/O. Example: lookup.Validate().
func (l ValueLookup[M, V]) Validate() error {
	if l.valueCore == nil {
		return invalid("model lookup requires a source and field")
	}
	if l.valueCore.err != nil {
		return l.valueCore.err
	}
	q := l.valueCore.state
	if q.table == nil || q.ast.Limit.IsSet() || q.ast.Offset != 0 || len(q.ast.Orders) != 0 || len(q.relations) != 0 || q.relationLimits != nil {
		return invalid("model lookup requires an unpaginated model scope without ordering or eager loading")
	}
	var zero V
	return validateQuery(where(q, []Predicate[M]{comparison[M](l.valueCore.field, querycore.Equal, []V{zero})}))
}

// Exists performs one equality observation without a transaction. Example: lookup.Exists(ctx, db, email).
func (l ValueLookup[M, V]) Exists(ctx context.Context, ex database.Executor, input V) (bool, error) {
	if err := l.Validate(); err != nil {
		return false, err
	}
	return (Query[M]{state: l.valueCore.state}).Where(comparison[M](l.valueCore.field, querycore.Equal, []V{input})).Exists(ctx, ex)
}

// AllExist checks SQL equality in batches of 64, including duplicate representations.
// Empty input performs no I/O. Example: lookup.AllExist(ctx, db, emails).
func (l ValueLookup[M, V]) AllExist(ctx context.Context, ex database.Executor, input []V) (bool, error) {
	return l.batch(ctx, ex, input, true)
}

// AnyExist checks IN predicates in batches of 64 and stops on the first match.
// Empty input performs no I/O. Example: lookup.AnyExist(ctx, db, emails).
func (l ValueLookup[M, V]) AnyExist(ctx context.Context, ex database.Executor, input []V) (bool, error) {
	return l.batch(ctx, ex, input, false)
}
func (l ValueLookup[M, V]) batch(ctx context.Context, ex database.Executor, input []V, all bool) (bool, error) {
	if err := l.Validate(); err != nil {
		return false, err
	}
	if ctx == nil {
		return false, invalid("model lookup requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	const batchSize = 64
	for start := 0; start < len(input); start += batchSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		batch := input[start:min(start+batchSize, len(input))]
		q := Query[M]{state: l.valueCore.state}
		if all {
			predicates := make([]Predicate[M], 0, len(batch))
			for _, item := range batch {
				inner := q.Where(comparison[M](l.valueCore.field, querycore.Equal, []V{item}))
				predicates = append(predicates, Predicate[M]{owner: q.state.table, expression: querycore.RetainPredicate(querycore.SubqueryPredicate{Query: querycore.Subquery{Node: inner.state.ast.ModelSelect(q.state.table.selections), Err: inner.state.err}})})
			}
			q = q.Where(predicates...)
		} else {
			q = q.Where(comparison[M](l.valueCore.field, querycore.In, batch))
		}
		found, err := q.Exists(ctx, ex)
		if err != nil {
			return false, err
		}
		if found != all {
			return found, nil
		}
	}
	return all, nil
}

// String redacts retained predicates. Example: fmt.Sprint(lookup).
func (ValueLookup[M, V]) String() string { return "model value lookup" }

// GoString redacts Go-syntax diagnostics. Example: fmt.Sprintf("%#v", lookup).
func (l ValueLookup[M, V]) GoString() string { return l.String() }

// Format redacts every formatting verb. Example: fmt.Sprintf("%+v", lookup).
func (l ValueLookup[M, V]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(l.String())) }
