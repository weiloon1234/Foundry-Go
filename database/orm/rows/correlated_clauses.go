package rows

import "github.com/weiloon1234/Foundry-Go/database/orm/expr"

// HavingCorrelatedRecord appends group predicates inside the correlated SELECT.
// Example: rows.HavingCorrelatedRecord(summary, count.Gt(0)).
func HavingCorrelatedRecord[O, I, R any](q CorrelatedRecord[O, I, R], predicates ...expr.HavingPredicate[Correlation[O, I]]) CorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = Having(Query[Correlation[O, I], struct{}]{source: q.core.Correlation.Source}, predicates...).source
	return q
}

// DistinctCorrelatedRecord removes duplicate inner results, replacing DISTINCT ON.
// Example: rows.DistinctCorrelatedRecord(selected).
func DistinctCorrelatedRecord[O, I, R any](q CorrelatedRecord[O, I, R]) CorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = Distinct(Query[Correlation[O, I], struct{}]{source: q.core.Correlation.Source}).source
	return q
}

// DistinctOnCorrelatedScalar selects one inner value per lifted row-key combination.
// Example: rows.DistinctOnCorrelatedScalar(selected, expr.Group(buyerID)).
func DistinctOnCorrelatedScalar[O, I, V any](q CorrelatedScalar[O, I, V], keys ...expr.GroupKey[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	q.core.Source = DistinctOn(Query[Correlation[O, I], struct{}]{source: q.core.Source}, keys...).source
	return q
}

// GroupByCorrelatedScalar appends lifted row keys inside the correlated SELECT.
// Example: rows.GroupByCorrelatedScalar(selected, expr.Group(bucket)).
func GroupByCorrelatedScalar[O, I, V any](q CorrelatedScalar[O, I, V], keys ...expr.GroupKey[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	q.core.Source = GroupBy(Query[Correlation[O, I], struct{}]{source: q.core.Source}, keys...).source
	return q
}

// DistinctOnCorrelatedScalarValues accepts selected keys inside the correlated SELECT.
// Example: rows.DistinctOnCorrelatedScalarValues(selected, count.Value().Key()).
func DistinctOnCorrelatedScalarValues[O, I, V any](q CorrelatedScalar[O, I, V], keys ...expr.ProjectionKey[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	q.core.Source = DistinctOnValues(Query[Correlation[O, I], struct{}]{source: q.core.Source}, keys...).source
	return q
}

// OrderCorrelatedScalar appends row or selected ordering inside the correlated SELECT.
// Example: rows.OrderCorrelatedScalar(selected, count.Desc()).
func OrderCorrelatedScalar[O, I, V any](q CorrelatedScalar[O, I, V], orders ...expr.ProjectionOrder[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	q.core.Source = recordOrders(q.core.Source, orders)
	return q
}
