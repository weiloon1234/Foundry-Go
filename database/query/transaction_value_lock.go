package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LockedTransactionValue retains its exact codec for transaction-only scalar,
// membership and EXISTS composition. Its explicit lock belongs to this SELECT.
type LockedTransactionValue[S TransactionScope, V any] struct {
	query LockedResult[S, V]
	codec codec.Codec[V]
}

func (q LockedTransactionValue[S, V]) transactionSubquery() subquery {
	return q.query.transactionSubquery()
}
func (q LockedTransactionValue[S, V]) transactionValue() valueSubquery[V] {
	return valueSubquery[V]{subquery: q.transactionSubquery(), codec: q.codec}
}
func (q LockedTransactionValue[S, V]) Where(predicates ...Predicate[S]) LockedTransactionValue[S, V] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q LockedTransactionValue[S, V]) OrderBy(orders ...ProjectionOrder[S]) LockedTransactionValue[S, V] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q LockedTransactionValue[S, V]) Limit(count int) LockedTransactionValue[S, V] {
	q.query = q.query.Limit(count)
	return q
}
func (q LockedTransactionValue[S, V]) Offset(count int) LockedTransactionValue[S, V] {
	q.query = q.query.Offset(count)
	return q
}

// Of derives a lock policy without changing the original value query.
func (q LockedTransactionValue[S, V]) Of(targets ...LockTarget[S]) LockedTransactionValue[S, V] {
	q.query = q.query.Of(targets...)
	return q
}

// NoWait derives a lock policy without changing the original value query.
func (q LockedTransactionValue[S, V]) NoWait() LockedTransactionValue[S, V] {
	q.query = q.query.NoWait()
	return q
}

// SkipLocked derives a lock policy without changing the original value query.
func (q LockedTransactionValue[S, V]) SkipLocked() LockedTransactionValue[S, V] {
	q.query = q.query.SkipLocked()
	return q
}

// Wait derives a lock policy without changing the original value query.
func (q LockedTransactionValue[S, V]) Wait() LockedTransactionValue[S, V] {
	q.query = q.query.Wait()
	return q
}

// Compile validates the locked SELECT without executing it.
func (q LockedTransactionValue[S, V]) Compile() (Statement, error) { return q.query.Compile() }

// All reads the exact value type through the caller's transaction.
func (q LockedTransactionValue[S, V]) All(ctx context.Context, tx *database.Tx) ([]V, error) {
	return q.query.All(ctx, tx)
}

// First reads the exact value type through the caller's transaction.
func (q LockedTransactionValue[S, V]) First(ctx context.Context, tx *database.Tx) (value.Optional[V], error) {
	return q.query.First(ctx, tx)
}

// RequireFirst reads the exact value type through the caller's transaction.
func (q LockedTransactionValue[S, V]) RequireFirst(ctx context.Context, tx *database.Tx) (V, error) {
	return q.query.RequireFirst(ctx, tx)
}

// Each reads the exact value type through the caller's transaction.
func (q LockedTransactionValue[S, V]) Each(ctx context.Context, tx *database.Tx, yield func(V) error) error {
	return q.query.Each(ctx, tx, yield)
}
