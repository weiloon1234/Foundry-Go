package query

// HavingPredicate filters groups and cannot be used as a row-level Predicate.
// Aggregate comparison methods and Grouped construct these values.
type HavingPredicate[S any] struct {
	_          [0]*S
	expression expression
}

// Grouped lifts a row predicate into HAVING. Every referenced ordinary column
// must appear in GROUP BY; Compile validates this before executing SQL.
func Grouped[S any](p Predicate[S]) HavingPredicate[S] {
	return HavingPredicate[S]{expression: p.expression}
}
func (p HavingPredicate[S]) Not() HavingPredicate[S] {
	return HavingPredicate[S]{expression: negation{p.expression}}
}

// HavingAnd requires all group predicates to match. An empty operand list is invalid.
func HavingAnd[S any](predicates ...HavingPredicate[S]) HavingPredicate[S] {
	return combineHaving(false, predicates)
}

// HavingOr requires at least one group predicate to match. An empty operand list is invalid.
func HavingOr[S any](predicates ...HavingPredicate[S]) HavingPredicate[S] {
	return combineHaving(true, predicates)
}
func combineHaving[S any](any bool, predicates []HavingPredicate[S]) HavingPredicate[S] {
	children := make([]expression, len(predicates))
	for i, p := range predicates {
		children[i] = p.expression
	}
	return HavingPredicate[S]{expression: junction{any, children}}
}
