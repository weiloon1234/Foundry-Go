package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionValueSource retains a concrete single-column result and all locks.
type TransactionValueSource[V any] interface {
	TransactionSubquerySource
	transactionValue() valueSubquery[V]
}

// TransactionValue lifts an ordinary single-column source for transaction composition.
type TransactionValue[V any] struct{ value valueSubquery[V] }

// TransactionValueOf explicitly lifts an ordinary value SELECT, retaining its codec.
func TransactionValueOf[V any](source ValueQuerySource[V]) TransactionValue[V] {
	return TransactionValue[V]{value: valueSource(source)}
}
func (q TransactionValue[V]) transactionValue() valueSubquery[V] { return q.value }
func (q TransactionValue[V]) transactionSubquery() subquery      { return q.value.subquery }

type transactionValueAdapter[V any] struct{ value valueSubquery[V] }

func (a transactionValueAdapter[V]) valueQuery() valueSubquery[V] { return a.value }
func transactionValueOf[V any](source TransactionValueSource[V]) valueSubquery[V] {
	if nilDescriptor(source) {
		return valueSubquery[V]{subquery: subquery{err: fault.New(fault.Invalid, "transaction value requires a source")}}
	}
	return source.transactionValue()
}

// TransactionValueQuery selects one exact value while requiring Tx for all reads.
type TransactionValueQuery[S TransactionScope, V any] struct{ query ValueQuery[S, V] }

// SelectTransactionValue reuses the ordinary single-column decoder and mappings.
// The transaction source and result have no unrestricted source conversion.
func SelectTransactionValue[S TransactionScope, V any](source TransactionProjectionSource[S], expression Expression[S, V]) TransactionValueQuery[S, V] {
	return TransactionValueQuery[S, V]{query: SelectValue(transactionProjection(source), expression)}
}
func (q TransactionValueQuery[S, V]) reader() TransactionQuery[S, V] {
	return TransactionQuery[S, V]{query: q.query.query}
}
func (q TransactionValueQuery[S, V]) transactionValue() valueSubquery[V] { return q.query.valueQuery() }
func (q TransactionValueQuery[S, V]) transactionSubquery() subquery      { return q.query.subquery() }
func (q TransactionValueQuery[S, V]) Where(predicates ...Predicate[S]) TransactionValueQuery[S, V] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q TransactionValueQuery[S, V]) OrderBy(orders ...ProjectionOrder[S]) TransactionValueQuery[S, V] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q TransactionValueQuery[S, V]) GroupBy(groups ...Group[S]) TransactionValueQuery[S, V] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q TransactionValueQuery[S, V]) Having(predicates ...HavingPredicate[S]) TransactionValueQuery[S, V] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q TransactionValueQuery[S, V]) Limit(count int) TransactionValueQuery[S, V] {
	q.query = q.query.Limit(count)
	return q
}
func (q TransactionValueQuery[S, V]) Offset(count int) TransactionValueQuery[S, V] {
	q.query = q.query.Offset(count)
	return q
}

// Compile validates the complete SELECT and retains nested locks.
func (q TransactionValueQuery[S, V]) Compile() (Statement, error) { return q.reader().Compile() }

// All requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) All(ctx context.Context, tx *database.Tx) ([]V, error) {
	return q.reader().All(ctx, tx)
}

// First requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) First(ctx context.Context, tx *database.Tx) (value.Optional[V], error) {
	return q.reader().First(ctx, tx)
}

// RequireFirst requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) RequireFirst(ctx context.Context, tx *database.Tx) (V, error) {
	return q.reader().RequireFirst(ctx, tx)
}

// Each requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) Each(ctx context.Context, tx *database.Tx, yield func(V) error) error {
	return q.reader().Each(ctx, tx, yield)
}

// Count requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) Count(ctx context.Context, tx *database.Tx) (int64, error) {
	return q.reader().Count(ctx, tx)
}

// Exists requires the caller's transaction and preserves the selected value type.
func (q TransactionValueQuery[S, V]) Exists(ctx context.Context, tx *database.Tx) (bool, error) {
	return q.reader().Exists(ctx, tx)
}

// ForUpdate locks this SELECT while retaining its typed single-column source.
func (q TransactionValueQuery[S, V]) ForUpdate() LockedTransactionValue[S, V] {
	return LockedTransactionValue[S, V]{query: q.query.ForUpdate(), codec: q.query.codec}
}

// ForNoKeyUpdate locks this SELECT while retaining its typed single-column source.
func (q TransactionValueQuery[S, V]) ForNoKeyUpdate() LockedTransactionValue[S, V] {
	return LockedTransactionValue[S, V]{query: q.query.ForNoKeyUpdate(), codec: q.query.codec}
}

// ForShare locks this SELECT while retaining its typed single-column source.
func (q TransactionValueQuery[S, V]) ForShare() LockedTransactionValue[S, V] {
	return LockedTransactionValue[S, V]{query: q.query.ForShare(), codec: q.query.codec}
}

// ForKeyShare locks this SELECT while retaining its typed single-column source.
func (q TransactionValueQuery[S, V]) ForKeyShare() LockedTransactionValue[S, V] {
	return LockedTransactionValue[S, V]{query: q.query.ForKeyShare(), codec: q.query.codec}
}

// LockRows applies independent table policies to this SELECT.
func (q TransactionValueQuery[S, V]) LockRows(clauses ...RowLock[S]) LockedTransactionValue[S, V] {
	return LockedTransactionValue[S, V]{query: q.query.LockRows(clauses...), codec: q.query.codec}
}
