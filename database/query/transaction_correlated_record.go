package query

// TransactionCorrelatedRecordQuery preserves a complete result and declared
// parent scope. It has no standalone terminals or unrestricted source conversion.
type TransactionCorrelatedRecordQuery[O, I TransactionScope, R any] struct {
	query ProjectionQuery[TransactionCorrelation[O, I], R]
	outer scopeRequirement
}

// SelectTransactionCorrelatedRecord selects a complete preserved inner record.
func SelectTransactionCorrelatedRecord[O, I TransactionScope, R any](source TransactionCorrelatedSource[O, I], scope RecordScope[TransactionCorrelation[O, I], R]) TransactionCorrelatedRecordQuery[O, I, R] {
	return TransactionCorrelatedRecordQuery[O, I, R]{query: SelectRecord(source.source, scope), outer: source.outer.scopeRequirement}
}

// ProjectTransactionCorrelated is the generated projection boundary for a
// transaction-required correlated record. Mapping and decoding remain shared.
func ProjectTransactionCorrelated[O, I TransactionScope, R any](source TransactionCorrelatedSource[O, I], definition ProjectionDefinition[R], mappings ...ProjectionMapping[TransactionCorrelation[O, I], R]) TransactionCorrelatedRecordQuery[O, I, R] {
	return TransactionCorrelatedRecordQuery[O, I, R]{query: Project(source.source, definition, mappings...), outer: source.outer.scopeRequirement}
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) Where(predicates ...Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) OrderBy(orders ...ProjectionOrder[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) GroupBy(groups ...Group[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) Having(predicates ...HavingPredicate[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) Limit(count int) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Limit(count)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) Offset(count int) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Offset(count)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) Distinct() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Distinct()
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) DistinctOn(keys ...Group[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.DistinctOn(keys...)
	return q
}
func (q TransactionCorrelatedRecordQuery[O, I, R]) DistinctOnValues(keys ...ProjectionKey[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = q.query.DistinctOnValues(keys...)
	return q
}

// TransactionCorrelatedRecordSource retains a declared parent and complete record.
type TransactionCorrelatedRecordSource[O TransactionScope, R any] interface{ transactionCorrelatedRecord() correlatedRecord[O, R] }

func (q TransactionCorrelatedRecordQuery[O, I, R]) transactionCorrelatedRecord() correlatedRecord[O, R] {
	return correlatedRecord[O, R]{query: q.query.recordQuery(), outer: q.outer}
}

// Exists yields a Tx-owned parent predicate and retains this SELECT's locks.
func (q TransactionCorrelatedRecordQuery[O, I, R]) Exists() Predicate[O] {
	r := q.transactionCorrelatedRecord()
	return Predicate[O]{expression: subqueryPredicate{query: subquery{node: r.query.node, err: r.query.err, correlation: &r.outer}}}
}
