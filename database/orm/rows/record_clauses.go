package rows

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

// DistinctRecord removes duplicate complete records without adding model writes.
// Example: rows.DistinctRecord(rows.SelectRecord(joined, scope)).
func DistinctRecord[S, R any](q RecordRead[S, R]) RecordRead[S, R] {
	q.core.Source = Distinct(Query[S, struct{}]{source: q.core.Source}).source
	q.core.Direct = false
	return q
}

// DistinctSet removes duplicate combined records, preserving both input SELECTs.
// Example: rows.DistinctSet(rows.UnionAll(left, right)).
func DistinctSet[R any](q SetQuery[R]) SetQuery[R] {
	q.core.Source = Distinct(Query[Set[R], struct{}]{source: q.core.Source}).source
	q.core.Direct = false
	return q
}

// DistinctValues removes duplicate combined values with their original codec.
// Example: rows.DistinctValues(rows.UnionAllValues(left, right)).
func DistinctValues[V any](q ValueSetQuery[V]) ValueSetQuery[V] {
	q.core.Source = DistinctScalar(SelectScalar(q, expr.AsSelected(expr.Row(q.Value())))).source
	q.core.Direct = false
	return q
}

// DistinctOnValueSet selects one combined value per selected-key combination.
// Keys and ordering belong to the combined output scope, not either input SELECT.
// Example: rows.DistinctOnValueSet(values, expr.Key(expr.AsSelected(expr.Row(values.Value())))).
func DistinctOnValueSet[V any](q ValueSetQuery[V], keys ...expr.ProjectionKey[Set[V]]) ValueSetQuery[V] {
	q.core.Source = DistinctOnScalarValues(SelectScalar(q, expr.AsSelected(expr.Row(q.Value()))), keys...).source
	q.core.Direct = false
	return q
}

// DistinctOnRecord selects a complete read-only record per input-key combination.
// Ordering stays in the input scope, including columns not in the selected record.
// Example: rows.DistinctOnRecord(rows.SelectRecord(joined,left),expr.Group(buyerID)).OrderBy(expr.Asc(buyerID),expr.Desc(orderID)).
func DistinctOnRecord[S, R any](q RecordRead[S, R], keys ...expr.GroupKey[S]) RecordRead[S, R] {
	q.core.Source = DistinctOn(Query[S, struct{}]{source: q.core.Source}, keys...).source
	q.core.Direct = false
	return q
}
