package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionQuery selects a complete result R from transaction-owned input S.
// It has no ordinary source conversion or model mutations. All reads require Tx.
type TransactionQuery[S TransactionScope, R any] struct{ query ProjectionQuery[S, R] }

// ProjectTransaction is the generated projection boundary for transaction sources.
// It reuses ordinary result declarations, exact mappings and complete decoders.
func ProjectTransaction[S TransactionScope, R any](source TransactionProjectionSource[S], definition ProjectionDefinition[R], mappings ...ProjectionMapping[S, R]) TransactionQuery[S, R] {
	return TransactionQuery[S, R]{query: Project(transactionProjection(source), definition, mappings...)}
}

// SelectTransactionRecord selects every declared field from a preserved scope.
func SelectTransactionRecord[S TransactionScope, R any](source TransactionProjectionSource[S], scope RecordScope[S, R]) TransactionQuery[S, R] {
	return TransactionQuery[S, R]{query: SelectRecord(transactionProjection(source), scope)}
}
func (q TransactionQuery[S, R]) Where(predicates ...Predicate[S]) TransactionQuery[S, R] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q TransactionQuery[S, R]) OrderBy(orders ...ProjectionOrder[S]) TransactionQuery[S, R] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q TransactionQuery[S, R]) GroupBy(groups ...Group[S]) TransactionQuery[S, R] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q TransactionQuery[S, R]) Having(predicates ...HavingPredicate[S]) TransactionQuery[S, R] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q TransactionQuery[S, R]) Limit(count int) TransactionQuery[S, R] {
	q.query = q.query.Limit(count)
	return q
}
func (q TransactionQuery[S, R]) Offset(count int) TransactionQuery[S, R] {
	q.query = q.query.Offset(count)
	return q
}
func (q TransactionQuery[S, R]) reader() readResult[R] {
	r := q.query.reader()
	r.transaction = true
	return r
}
func (q TransactionQuery[S, R]) transactionRecord() recordQuery[R] { return q.query.recordQuery() }

// Compile retains all nested locks and validates the complete statement.
func (q TransactionQuery[S, R]) Compile() (Statement, error) { return q.reader().Compile() }

// All collects complete records, discarding partial results on failure.
func (q TransactionQuery[S, R]) All(ctx context.Context, tx *database.Tx) ([]R, error) {
	if err := lockContext(ctx, tx); err != nil {
		return nil, err
	}
	return q.reader().All(ctx, tx)
}

// First returns an absent Optional when no record matches. Order explicitly.
func (q TransactionQuery[S, R]) First(ctx context.Context, tx *database.Tx) (value.Optional[R], error) {
	if err := lockContext(ctx, tx); err != nil {
		return value.Optional[R]{}, err
	}
	return q.reader().First(ctx, tx)
}

// RequireFirst reports database.NotFound when no record matches.
func (q TransactionQuery[S, R]) RequireFirst(ctx context.Context, tx *database.Tx) (R, error) {
	if err := lockContext(ctx, tx); err != nil {
		return *new(R), err
	}
	return q.reader().RequireFirst(ctx, tx)
}

// Each streams complete records with the transaction's rows open. Use All
// before additional database operations on the same transaction.
func (q TransactionQuery[S, R]) Each(ctx context.Context, tx *database.Tx, yield func(R) error) error {
	if err := lockContext(ctx, tx); err != nil {
		return err
	}
	return q.reader().Each(ctx, tx, yield)
}

// Count counts the selected result window; nested source locks remain in place.
func (q TransactionQuery[S, R]) Count(ctx context.Context, tx *database.Tx) (int64, error) {
	if err := lockContext(ctx, tx); err != nil {
		return 0, err
	}
	return q.reader().Count(ctx, tx)
}

// Exists checks for a result without discarding locks in source definitions.
func (q TransactionQuery[S, R]) Exists(ctx context.Context, tx *database.Tx) (bool, error) {
	if err := lockContext(ctx, tx); err != nil {
		return false, err
	}
	return q.reader().Exists(ctx, tx)
}

// ForUpdate locks rows in this SELECT in addition to locks in its sources.
// CTE references themselves cannot be outer lock targets.
func (q TransactionQuery[S, R]) ForUpdate() LockedResult[S, R] { return q.query.ForUpdate() }

// ForNoKeyUpdate locks this SELECT's rows while allowing compatible key-share reads.
func (q TransactionQuery[S, R]) ForNoKeyUpdate() LockedResult[S, R] { return q.query.ForNoKeyUpdate() }

// ForShare requests shared row locks in this SELECT.
func (q TransactionQuery[S, R]) ForShare() LockedResult[S, R] { return q.query.ForShare() }

// ForKeyShare prevents deletion and key-changing updates to this SELECT's rows.
func (q TransactionQuery[S, R]) ForKeyShare() LockedResult[S, R] { return q.query.ForKeyShare() }

// LockRows declares independent locking clauses for this SELECT's preserved inputs.
func (q TransactionQuery[S, R]) LockRows(clauses ...RowLock[S]) LockedResult[S, R] {
	return q.query.LockRows(clauses...)
}
