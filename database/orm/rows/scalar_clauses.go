package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OrderScalar appends row or selected ordering inside the scalar SELECT.
// Example: rows.OrderScalar(selected, age.Asc()).
func OrderScalar[S, V any](q Scalar[S, V], orders ...expr.ProjectionOrder[S]) Scalar[S, V] {
	q.source = recordOrders(q.source, orders)
	return q
}

// LimitScalar replaces the inner limit without changing scalar cardinality rules.
// Example: rows.LimitScalar(selected, 1).
func LimitScalar[S, V any](q Scalar[S, V], n int) Scalar[S, V] {
	q.source.Node.Limit = value.Set(n)
	return q
}

// OffsetScalar replaces the offset inside the scalar SELECT.
// Example: rows.OffsetScalar(selected, 1).
func OffsetScalar[S, V any](q Scalar[S, V], n int) Scalar[S, V] {
	q.source.Node.Offset = n
	return q
}
