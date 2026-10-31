package orm

// TextExpressionColumn preserves text operations at the expression boundary.
// The interface excludes direct non-text inputs. Adapters also validate the
// retrieved column metadata: embedding can promote markers from another handle.
// Example: expr.TextContains(expr.TextRow(orm.TextExpr(Users.Name)), "Ada").
type TextExpressionColumn[M, V, P any] interface {
	OrderedExpressionColumn[M, V, P]
	ormContains(V) Predicate[M]
}
