package query

// Text wrappers attest the compiler capability at their existing typed boundary.
// Scalar comparisons never acquire it just because their codec stores a string.
func (f TextField[M, V]) compare(op operator, values ...V) Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, op, f.codec, values, true)}
}
func (f NullableTextField[M, V]) compare(op operator, values ...V) Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, op, f.codec, values, true)}
}
func (e TextRowExpression[S, V]) compare(op operator, values ...V) Predicate[S] {
	return Predicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, values, true)}
}
func (e NullableTextRowExpression[S, V]) comparePresent(op operator, values ...V) Predicate[S] {
	return Predicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, presentValues(values), true)}
}
func (e TextValueComparison[S, V]) compare(op operator, values ...V) HavingPredicate[S] {
	return HavingPredicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, values, true)}
}
func (e NullableTextValueComparison[S, V]) comparePresent(op operator, values ...V) HavingPredicate[S] {
	return HavingPredicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, presentValues(values), true)}
}
