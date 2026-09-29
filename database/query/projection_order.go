package query

// ProjectionOrder preserves input scope for field or expression ordering.
// Model queries accept row Order values; selected aggregate/window ordering
// remains available only through the read-only projection/result boundary.
type ProjectionOrder[S any] interface{ projectionOrder() expressionOrder[S] }

type expressionOrder[S any] struct {
	_    [0]*S
	node orderNode
}

func (o expressionOrder[S]) projectionOrder() expressionOrder[S] { return o }
func (o Order[S]) projectionOrder() expressionOrder[S] {
	return expressionOrder[S]{node: orderNode{o.value(), o.descending, o.nulls}}
}

// Asc orders a projected expression. It can be combined with ordinary field orders.
func (e Expression[S, V]) Asc() ProjectionOrder[S] {
	return expressionOrder[S]{node: orderNode{expression: e.node}}
}
func (e Expression[S, V]) Desc() ProjectionOrder[S] {
	return expressionOrder[S]{node: orderNode{expression: e.node, descending: true}}
}
func (a Aggregate[M, V]) Asc() ProjectionOrder[M]  { return a.Value().Asc() }
func (a Aggregate[M, V]) Desc() ProjectionOrder[M] { return a.Value().Desc() }
