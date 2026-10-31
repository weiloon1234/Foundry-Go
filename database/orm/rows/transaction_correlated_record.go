package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionCorrelatedRecord keeps a complete record bound to its enclosing scope. Example: rows.SelectTransactionCorrelatedRecord(c,rows.TransactionInnerScope(c,scope)).
type TransactionCorrelatedRecord[O, I TransactionScopeType, R any] struct {
	_                      [0]*O
	_                      [0]*I
	_                      [0]*R
	transactionRecordPhase [0]struct{}
	core                   ormcore.CorrelatedRecord
}

// SelectTransactionCorrelatedRecord selects a complete preserved inner model. Example: rows.SelectTransactionCorrelatedRecord(c,rows.TransactionInnerScope(c,rows.TransactionScope(child))).
func SelectTransactionCorrelatedRecord[O, I TransactionScopeType, R any](c TransactionCorrelatedSource[O, I], scope expr.Scope[TransactionCorrelation[O, I], R]) TransactionCorrelatedRecord[O, I, R] {
	return TransactionCorrelatedRecord[O, I, R]{core: ormcore.SelectCorrelationRecord(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()))}
}

// ProjectTransactionCorrelated maps a complete declared record without granting execution. Example: rows.ProjectTransactionCorrelated(c,Totals,rows.MapSelected(Totals.Count,count)).
func ProjectTransactionCorrelated[O, I TransactionScopeType, R any](c TransactionCorrelatedSource[O, I], schema Schema[R], mappings ...Mapping[TransactionCorrelation[O, I], R]) TransactionCorrelatedRecord[O, I, R] {
	return TransactionCorrelatedRecord[O, I, R]{core: projectCorrelation(c.core, schema, mappings)}
}

// Where appends lifted inner predicates. Example: record.Where(expr.Equal(childID,parentID)).
func (r TransactionCorrelatedRecord[O, I, R]) Where(ps ...expr.Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = whereSource(r.core.Correlation.Source, ps)
	return r
}

// OrderBy appends inner result ordering. Example: record.OrderBy(expr.Desc(childID)).
func (r TransactionCorrelatedRecord[O, I, R]) OrderBy(os ...expr.ProjectionOrder[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = recordOrders(r.core.Correlation.Source, os)
	return r
}

// Limit retains the record's inner limit. Example: record.Limit(1).
func (r TransactionCorrelatedRecord[O, I, R]) Limit(n int) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source.Node.Limit = value.Set(n)
	return r
}

// Offset retains the record's inner offset. Example: record.Offset(1).
func (r TransactionCorrelatedRecord[O, I, R]) Offset(n int) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source.Node.Offset = n
	return r
}

// Exists tests the record within its required outer scope. Example: record.Exists().
func (r TransactionCorrelatedRecord[O, I, R]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(r.core.Correlation), sqlowner.Expressions())
}

// GroupBy retains the correlated result clause. Example: selected.GroupBy(clause).
func (r TransactionCorrelatedRecord[O, I, R]) GroupBy(keys ...expr.GroupKey[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = GroupBy(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Correlation.Source}, keys...).source
	return r
}

// Having retains the correlated result clause. Example: selected.Having(clause).
func (r TransactionCorrelatedRecord[O, I, R]) Having(ps ...expr.HavingPredicate[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = Having(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Correlation.Source}, ps...).source
	return r
}

// Distinct retains the correlated result clause. Example: selected.Distinct().
func (r TransactionCorrelatedRecord[O, I, R]) Distinct() TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = Distinct(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Correlation.Source}).source
	return r
}

// DistinctOn retains the correlated result clause. Example: selected.DistinctOn(clause).
func (r TransactionCorrelatedRecord[O, I, R]) DistinctOn(keys ...expr.GroupKey[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = DistinctOn(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Correlation.Source}, keys...).source
	return r
}

// DistinctOnValues retains the correlated result clause. Example: selected.DistinctOnValues(clause).
func (r TransactionCorrelatedRecord[O, I, R]) DistinctOnValues(keys ...expr.ProjectionKey[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = DistinctOnValues(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Correlation.Source}, keys...).source
	return r
}
