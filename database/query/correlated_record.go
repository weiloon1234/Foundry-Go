package query

// CorrelatedRecordQuery selects a complete model or declared projection while
// retaining its required outer scope. It has no standalone execution methods.
type CorrelatedRecordQuery[O, I, R any] struct {
	query ProjectionQuery[Correlation[O, I], R]
	outer scopeRequirement
}

// SelectCorrelatedRecord selects a complete preserved inner record with its
// original decoder. Nullable inner sides require a declared projection instead.
func SelectCorrelatedRecord[O, I, R any](source CorrelatedSource[O, I], scope RecordScope[Correlation[O, I], R]) CorrelatedRecordQuery[O, I, R] {
	return CorrelatedRecordQuery[O, I, R]{query: SelectRecord(source.source, scope), outer: source.outer.scopeRequirement}
}

// ProjectCorrelated is the generated projection boundary for lateral records.
// It shares complete mapping validation with Project without exposing an
// independently executable query that could lose its outer requirement.
func ProjectCorrelated[O, I, R any](source CorrelatedSource[O, I], definition ProjectionDefinition[R], mappings ...ProjectionMapping[Correlation[O, I], R]) CorrelatedRecordQuery[O, I, R] {
	return CorrelatedRecordQuery[O, I, R]{query: Project(source.source, definition, mappings...), outer: source.outer.scopeRequirement}
}

func (q CorrelatedRecordQuery[O, I, R]) Where(predicates ...Predicate[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) GroupBy(groups ...Group[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) Having(predicates ...HavingPredicate[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) OrderBy(orders ...ProjectionOrder[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) Limit(count int) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Limit(count)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) Offset(count int) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Offset(count)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) Distinct() CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.Distinct()
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) DistinctOn(keys ...Group[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.DistinctOn(keys...)
	return q
}
func (q CorrelatedRecordQuery[O, I, R]) DistinctOnValues(keys ...ProjectionKey[Correlation[O, I]]) CorrelatedRecordQuery[O, I, R] {
	q.query = q.query.DistinctOnValues(keys...)
	return q
}

// Exists keeps the selected record's grouping, filters and result window.
func (q CorrelatedRecordQuery[O, I, R]) Exists() Predicate[O] {
	r := q.correlatedRecord()
	return Predicate[O]{expression: subqueryPredicate{query: subquery{node: r.query.node, err: r.query.err, correlation: &r.outer}}}
}

// CorrelatedRecordSource retains both the outer scope and complete record type.
// It deliberately does not implement RecordQuerySource or ProjectionSource.
type CorrelatedRecordSource[O, R any] interface{ correlatedRecord() correlatedRecord[O, R] }
type correlatedRecord[O, R any] struct {
	_     [0]*O
	query recordQuery[R]
	outer scopeRequirement
}

func (q CorrelatedRecordQuery[O, I, R]) correlatedRecord() correlatedRecord[O, R] {
	return correlatedRecord[O, R]{query: q.query.recordQuery(), outer: q.outer}
}
