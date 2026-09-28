package query

// IContains performs PostgreSQL case-insensitive literal substring matching.
// Wildcards and the escape character remain literal, using the same compiler
// escaping as Contains. Case handling follows the database's active collation.
func (f TextField[M, V]) IContains(text V) Predicate[M] { return f.compare(insensitiveContains, text) }

// IContains retains SQL NULL and the field's exact non-null comparison type.
func (f NullableTextField[M, V]) IContains(text V) Predicate[M] {
	return f.compare(insensitiveContains, text)
}

// IContains matches a literal substring in a typed row expression.
func (e TextRowExpression[S, V]) IContains(text V) Predicate[S] {
	return e.compare(insensitiveContains, text)
}

// IContains matches a literal substring while retaining SQL NULL in results.
func (e NullableTextRowExpression[S, V]) IContains(text V) Predicate[S] {
	return e.comparePresent(insensitiveContains, text)
}

// IContains creates a case-insensitive literal group predicate.
func (e TextValueComparison[S, V]) IContains(text V) HavingPredicate[S] {
	return e.compare(insensitiveContains, text)
}

// IContains creates a nullable case-insensitive literal group predicate.
func (e NullableTextValueComparison[S, V]) IContains(text V) HavingPredicate[S] {
	return e.comparePresent(insensitiveContains, text)
}
