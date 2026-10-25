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
	q.core.Source = DistinctScalar(SelectScalar(q, expr.Select(q.Value()))).source
	q.core.Direct = false
	return q
}
