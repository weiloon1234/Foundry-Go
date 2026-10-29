package orm

// OrderedExpressionColumn is the sealed capability for ordered stored fields.
// The private marker also covers input handles: aggregates read their stored value.
// Example: func minimum[M,V,P any,C orm.OrderedExpressionColumn[M,V,P]](c C).
type OrderedExpressionColumn[M, V, P any] interface {
	ExpressionColumn[M, V, P]
	orderedColumn()
}
