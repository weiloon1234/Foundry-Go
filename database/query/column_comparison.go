package query

// OrderedKeyField is a generated column with range comparison capabilities.
// Scalar enums and arbitrary comparable keys cannot be used for column ranges.
type OrderedKeyField[M any, V comparable] interface {
	KeyField[M, V]
	orderedKey() ScalarField[M, V]
}

func (f OrderedField[M, V]) orderedKey() ScalarField[M, V]         { return f.ScalarField }
func (f NullableOrderedField[M, V]) orderedKey() ScalarField[M, V] { return f.ScalarField }

// EqColumn compares columns in one explicit scope with compatible value types.
// Nullable columns keep their base type; SQL NULL equality remains unknown.
func (f ScalarField[M, V]) EqColumn(other KeyField[M, V]) Predicate[M] {
	return f.compareColumn(equal, other)
}
func (f ScalarField[M, V]) NeColumn(other KeyField[M, V]) Predicate[M] {
	return f.compareColumn(notEqual, other)
}
func (f ScalarField[M, V]) compareColumn(op operator, other KeyField[M, V]) Predicate[M] {
	if nilDescriptor(other) {
		return Predicate[M]{}
	}
	return Predicate[M]{expression: binaryComparison{f.ref, other.relationKey().ref, op}}
}
func (f OrderedField[M, V]) LtColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(less, other)
}
func (f OrderedField[M, V]) LteColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(lessOrEqual, other)
}
func (f OrderedField[M, V]) GtColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(greater, other)
}
func (f OrderedField[M, V]) GteColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(greaterOrEqual, other)
}
func (f NullableOrderedField[M, V]) LtColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(less, other)
}
func (f NullableOrderedField[M, V]) LteColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(lessOrEqual, other)
}
func (f NullableOrderedField[M, V]) GtColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(greater, other)
}
func (f NullableOrderedField[M, V]) GteColumn(other OrderedKeyField[M, V]) Predicate[M] {
	return f.compareColumn(greaterOrEqual, other)
}
