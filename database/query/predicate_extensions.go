package query

// NotIn excludes the listed values. SQL NULL never satisfies NotIn; combine it
// with IsNull on nullable operands when NULL rows must also match. An empty
// list matches every non-excluded row (TRUE).
func (f valueField[M, V]) NotIn(values ...V) Predicate[M] { return f.compare(notIn, values...) }

// NotIn excludes the listed values; SQL NULL never matches.
func (e RowExpression[S, V]) NotIn(v ...V) Predicate[S] { return e.compare(notIn, v...) }

// NotIn excludes the listed values; SQL NULL never matches.
func (e NullableOrderedRowExpression[S, V]) NotIn(v ...V) Predicate[S] {
	return e.comparePresent(notIn, v...)
}

// Between matches the inclusive range low <= field <= high.
func (f OrderedField[M, V]) Between(low, high V) Predicate[M] {
	return And(f.Gte(low), f.Lte(high))
}

// Between matches the inclusive range; SQL NULL never matches.
func (f NullableOrderedField[M, V]) Between(low, high V) Predicate[M] {
	return And(f.Gte(low), f.Lte(high))
}

// Between matches the inclusive range low <= value <= high.
func (e OrderedRowExpression[S, V]) Between(low, high V) Predicate[S] {
	return And(e.Gte(low), e.Lte(high))
}

// Between matches the inclusive range; SQL NULL never matches.
func (e NullableOrderedRowExpression[S, V]) Between(low, high V) Predicate[S] {
	return And(e.Gte(low), e.Lte(high))
}

// StartsWith, EndsWith and their case-insensitive forms match literal text:
// the compiler escapes %, _ and its escape character. ILike accepts a raw
// case-insensitive SQL pattern, like Like.
func (f TextField[M, V]) StartsWith(text V) Predicate[M] { return f.compare(startsWith, text) }
func (f TextField[M, V]) EndsWith(text V) Predicate[M]   { return f.compare(endsWith, text) }
func (f TextField[M, V]) IStartsWith(text V) Predicate[M] {
	return f.compare(insensitiveStartsWith, text)
}
func (f TextField[M, V]) IEndsWith(text V) Predicate[M] { return f.compare(insensitiveEndsWith, text) }
func (f TextField[M, V]) ILike(pattern V) Predicate[M]  { return f.compare(insensitiveLike, pattern) }

func (f NullableTextField[M, V]) StartsWith(text V) Predicate[M] { return f.compare(startsWith, text) }
func (f NullableTextField[M, V]) EndsWith(text V) Predicate[M]   { return f.compare(endsWith, text) }
func (f NullableTextField[M, V]) IStartsWith(text V) Predicate[M] {
	return f.compare(insensitiveStartsWith, text)
}
func (f NullableTextField[M, V]) IEndsWith(text V) Predicate[M] {
	return f.compare(insensitiveEndsWith, text)
}
func (f NullableTextField[M, V]) ILike(pattern V) Predicate[M] {
	return f.compare(insensitiveLike, pattern)
}

func (e TextRowExpression[S, V]) StartsWith(v V) Predicate[S] { return e.compare(startsWith, v) }
func (e TextRowExpression[S, V]) EndsWith(v V) Predicate[S]   { return e.compare(endsWith, v) }
func (e TextRowExpression[S, V]) IStartsWith(v V) Predicate[S] {
	return e.compare(insensitiveStartsWith, v)
}
func (e TextRowExpression[S, V]) IEndsWith(v V) Predicate[S] {
	return e.compare(insensitiveEndsWith, v)
}
func (e TextRowExpression[S, V]) ILike(v V) Predicate[S] { return e.compare(insensitiveLike, v) }

func (e NullableTextRowExpression[S, V]) StartsWith(v V) Predicate[S] {
	return e.comparePresent(startsWith, v)
}
func (e NullableTextRowExpression[S, V]) EndsWith(v V) Predicate[S] {
	return e.comparePresent(endsWith, v)
}
func (e NullableTextRowExpression[S, V]) IStartsWith(v V) Predicate[S] {
	return e.comparePresent(insensitiveStartsWith, v)
}
func (e NullableTextRowExpression[S, V]) IEndsWith(v V) Predicate[S] {
	return e.comparePresent(insensitiveEndsWith, v)
}
func (e NullableTextRowExpression[S, V]) ILike(v V) Predicate[S] {
	return e.comparePresent(insensitiveLike, v)
}

// NullsFirst and NullsLast place SQL NULL explicitly. PostgreSQL's default is
// NULLS LAST for ascending and NULLS FIRST for descending order. Keyset and
// cursor traversal accept only the default placement.
func (o Order[M]) NullsFirst() Order[M] { o.nulls = nullsFirst; return o }
func (o Order[M]) NullsLast() Order[M]  { o.nulls = nullsLast; return o }

type nullPlacement uint8

const (
	nullsDefault nullPlacement = iota
	nullsFirst
	nullsLast
)

// orderDirection renders direction and any explicit NULL placement.
func orderDirection(order orderNode) string {
	direction := " ASC"
	if order.descending {
		direction = " DESC"
	}
	switch order.nulls {
	case nullsFirst:
		direction += " NULLS FIRST"
	case nullsLast:
		direction += " NULLS LAST"
	}
	return direction
}
