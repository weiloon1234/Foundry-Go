package query

// ProjectionKey is a selected value used by DISTINCT ON or a window partition.
// It is distinct from a row Group: aggregate values cannot enter GROUP BY.
type ProjectionKey[S any] struct {
	_    [0]*S
	node valueExpression
}

// Group makes this computed row value a scope-owned grouping key. Grouping by
// a computed value does not independently group the columns used inside it.
func (e RowExpression[S, V]) Group() Group[S] { return Group[S]{node: e.expression.node} }

// Key keeps this expression's scope while permitting mixed result types in a
// selected key list. Windows still cannot nest in another window's partition.
func (e Expression[S, V]) Key() ProjectionKey[S] { return ProjectionKey[S]{node: e.node} }

func (g Group[S]) keyExpression() valueExpression         { return g.node }
func (g ProjectionKey[S]) keyExpression() valueExpression { return g.node }

func keyExpressions[K interface{ keyExpression() valueExpression }](keys []K) []valueExpression {
	result := make([]valueExpression, len(keys))
	for i, key := range keys {
		result[i] = key.keyExpression()
	}
	return result
}
