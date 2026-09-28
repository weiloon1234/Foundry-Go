package query

type distinctKind uint8

const (
	noDistinct distinctKind = iota
	distinctRows
	distinctOn
)

type distinctSpec struct {
	kind distinctKind
	keys []valueExpression
}

// Distinct removes duplicate selected records before Limit/Offset. It replaces
// any earlier DistinctOn clause without changing the declared result type.
func (q ProjectionQuery[S, P]) Distinct() ProjectionQuery[S, P] {
	q.source.node.distinct = distinctSpec{kind: distinctRows}
	return q
}

// DistinctOn selects one row per combination of row keys (PostgreSQL).
// Use field.Group() or computed row.Group(). Order by those keys first, then by the desired
// row precedence and a unique tie-breaker. Without ordering the choice is unspecified.
// It replaces any earlier Distinct/DistinctOn clause.
func (q ProjectionQuery[S, P]) DistinctOn(keys ...Group[S]) ProjectionQuery[S, P] {
	return q.distinctOn(keyExpressions(keys))
}

// DistinctOnValues accepts selected keys, including aggregate and window
// results. Use Expression.Key; ordinary grouping and ordering rules still apply.
func (q ProjectionQuery[S, P]) DistinctOnValues(keys ...ProjectionKey[S]) ProjectionQuery[S, P] {
	return q.distinctOn(keyExpressions(keys))
}

func (q ProjectionQuery[S, P]) distinctOn(keys []valueExpression) ProjectionQuery[S, P] {
	q.source.node.distinct = distinctSpec{kind: distinctOn, keys: keys}
	return q
}

// Distinct selects complete, distinct models through a read-only query. Eager
// loading must be performed explicitly after collection; model writes are not available.
func (q Query[M]) Distinct() ProjectionQuery[M, M] {
	return SelectRecord(q, q.Scope()).Distinct()
}

// DistinctOn selects one complete model per key combination through a read-only
// query. See ProjectionQuery.DistinctOn for ordering requirements.
func (q Query[M]) DistinctOn(keys ...Group[M]) ProjectionQuery[M, M] {
	return SelectRecord(q, q.Scope()).DistinctOn(keys...)
}

// DistinctOnValues selects complete models using selected expression keys.
func (q Query[M]) DistinctOnValues(keys ...ProjectionKey[M]) ProjectionQuery[M, M] {
	return SelectRecord(q, q.Scope()).DistinctOnValues(keys...)
}

// Distinct removes duplicate complete records from this combined result.
func (q SetQuery[R]) Distinct() ProjectionQuery[Set[R], R] {
	return SelectRecord(q, q.Scope()).Distinct()
}

// DistinctOn selects one complete combined record per key combination.
func (q SetQuery[R]) DistinctOn(keys ...Group[Set[R]]) ProjectionQuery[Set[R], R] {
	return SelectRecord(q, q.Scope()).DistinctOn(keys...)
}

// DistinctOnValues selects complete combined records using selected keys.
func (q SetQuery[R]) DistinctOnValues(keys ...ProjectionKey[Set[R]]) ProjectionQuery[Set[R], R] {
	return SelectRecord(q, q.Scope()).DistinctOnValues(keys...)
}

// Distinct removes duplicate values while retaining the single-column contract.
func (q ValueQuery[S, V]) Distinct() ValueQuery[S, V] {
	q.query = q.query.Distinct()
	return q
}

// DistinctOn selects one value per key combination in its original input scope.
func (q ValueQuery[S, V]) DistinctOn(keys ...Group[S]) ValueQuery[S, V] {
	q.query = q.query.DistinctOn(keys...)
	return q
}

// DistinctOnValues selects one value per selected-key combination.
func (q ValueQuery[S, V]) DistinctOnValues(keys ...ProjectionKey[S]) ValueQuery[S, V] {
	q.query = q.query.DistinctOnValues(keys...)
	return q
}

// Distinct removes duplicate combined values, retaining their codec and type.
func (q ValueSetQuery[V]) Distinct() ValueQuery[Set[V], V] {
	return SelectValue(q, q.Value()).Distinct()
}

// DistinctOn selects combined values using row keys in the combined scope.
func (q ValueSetQuery[V]) DistinctOn(keys ...Group[Set[V]]) ValueQuery[Set[V], V] {
	return SelectValue(q, q.Value()).DistinctOn(keys...)
}

// DistinctOnValues selects combined values using selected expression keys.
func (q ValueSetQuery[V]) DistinctOnValues(keys ...ProjectionKey[Set[V]]) ValueQuery[Set[V], V] {
	return SelectValue(q, q.Value()).DistinctOnValues(keys...)
}

// Distinct removes duplicate inner values without erasing their outer ownership.
func (q CorrelatedValueQuery[O, I, V]) Distinct() CorrelatedValueQuery[O, I, V] {
	q.query = q.query.Distinct()
	return q
}

// DistinctOn selects one inner value per key combination, retaining correlation.
func (q CorrelatedValueQuery[O, I, V]) DistinctOn(keys ...Group[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.DistinctOn(keys...)
	return q
}

// DistinctOnValues retains explicit correlation when selecting by value keys.
func (q CorrelatedValueQuery[O, I, V]) DistinctOnValues(keys ...ProjectionKey[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.DistinctOnValues(keys...)
	return q
}
