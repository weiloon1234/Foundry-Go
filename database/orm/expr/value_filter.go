package expr

// ValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.ValueFilter(source)
type ValueFilterAdapter[S any, V any] struct {
	valueFilterPhase [0]struct{}
	_                [0]*S
	source           ValueOperand[S, V]
}

// ValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.ValueFilter(source)
func ValueFilter[S any, V any](source ValueOperand[S, V]) ValueFilterAdapter[S, V] {
	return ValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a ValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] { return EqValue(a.source, v) }

// Ne compares inequality within the adapter's HAVING scope.
func (a ValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] { return NeValue(a.source, v) }

// In tests membership within the adapter's HAVING scope.
func (a ValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] { return InValue(a.source, vs...) }

// NullableValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.NullableValueFilter(source)
type NullableValueFilterAdapter[S any, V any] struct {
	nullableValueFilterPhase [0]struct{}
	_                        [0]*S
	source                   interface {
		ValueOperand[S, V]
		NullValueOperand[S, V]
	}
}

// NullableValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.NullableValueFilter(source)
func NullableValueFilter[S any, V any](source interface {
	ValueOperand[S, V]
	NullValueOperand[S, V]
}) NullableValueFilterAdapter[S, V] {
	return NullableValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a NullableValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] { return EqValue(a.source, v) }

// Ne compares inequality within the adapter's HAVING scope.
func (a NullableValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] { return NeValue(a.source, v) }

// In tests membership within the adapter's HAVING scope.
func (a NullableValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] {
	return InValue(a.source, vs...)
}

// IsNull tests SQL nullness within the adapter's HAVING scope.
func (a NullableValueFilterAdapter[S, V]) IsNull() HavingPredicate[S] { return IsNullValue(a.source) }

// IsNotNull tests SQL presence within the adapter's HAVING scope.
func (a NullableValueFilterAdapter[S, V]) IsNotNull() HavingPredicate[S] {
	return IsNotNullValue(a.source)
}

// OrderValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.OrderValueFilter(source)
type OrderValueFilterAdapter[S any, V orderedScalar] struct {
	orderValueFilterPhase [0]struct{}
	_                     [0]*S
	source                OrderedValueOperand[S, V]
}

// OrderValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.OrderValueFilter(source)
func OrderValueFilter[S any, V orderedScalar](source OrderedValueOperand[S, V]) OrderValueFilterAdapter[S, V] {
	return OrderValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] { return EqValue(a.source, v) }

// Ne compares inequality within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] { return NeValue(a.source, v) }

// In tests membership within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] {
	return InValue(a.source, vs...)
}

// Lt compares less than within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Lt(v V) HavingPredicate[S] { return LtValue(a.source, v) }

// Lte compares less than or equal within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Lte(v V) HavingPredicate[S] { return LteValue(a.source, v) }

// Gt compares greater than within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Gt(v V) HavingPredicate[S] { return GtValue(a.source, v) }

// Gte compares greater than or equal within the adapter's HAVING scope.
func (a OrderValueFilterAdapter[S, V]) Gte(v V) HavingPredicate[S] { return GteValue(a.source, v) }

// NullableOrderValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.NullableOrderValueFilter(source)
type NullableOrderValueFilterAdapter[S any, V orderedScalar] struct {
	nullableOrderValueFilterPhase [0]struct{}
	_                             [0]*S
	source                        interface {
		OrderedValueOperand[S, V]
		NullValueOperand[S, V]
	}
}

// NullableOrderValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.NullableOrderValueFilter(source)
func NullableOrderValueFilter[S any, V orderedScalar](source interface {
	OrderedValueOperand[S, V]
	NullValueOperand[S, V]
}) NullableOrderValueFilterAdapter[S, V] {
	return NullableOrderValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] {
	return EqValue(a.source, v)
}

// Ne compares inequality within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] {
	return NeValue(a.source, v)
}

// In tests membership within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] {
	return InValue(a.source, vs...)
}

// Lt compares less than within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Lt(v V) HavingPredicate[S] {
	return LtValue(a.source, v)
}

// Lte compares less than or equal within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Lte(v V) HavingPredicate[S] {
	return LteValue(a.source, v)
}

// Gt compares greater than within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Gt(v V) HavingPredicate[S] {
	return GtValue(a.source, v)
}

// Gte compares greater than or equal within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) Gte(v V) HavingPredicate[S] {
	return GteValue(a.source, v)
}

// IsNull tests SQL nullness within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) IsNull() HavingPredicate[S] {
	return IsNullValue(a.source)
}

// IsNotNull tests SQL presence within the adapter's HAVING scope.
func (a NullableOrderValueFilterAdapter[S, V]) IsNotNull() HavingPredicate[S] {
	return IsNotNullValue(a.source)
}

// TextValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.TextValueFilter(source)
type TextValueFilterAdapter[S any, V ~string] struct {
	textValueFilterPhase [0]struct{}
	_                    [0]*S
	source               TextValueOperand[S, V]
}

// TextValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.TextValueFilter(source)
func TextValueFilter[S any, V ~string](source TextValueOperand[S, V]) TextValueFilterAdapter[S, V] {
	return TextValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] { return EqValue(a.source, v) }

// Ne compares inequality within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] { return NeValue(a.source, v) }

// In tests membership within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] { return InValue(a.source, vs...) }

// Lt compares less than within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Lt(v V) HavingPredicate[S] { return LtValue(a.source, v) }

// Lte compares less than or equal within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Lte(v V) HavingPredicate[S] { return LteValue(a.source, v) }

// Gt compares greater than within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Gt(v V) HavingPredicate[S] { return GtValue(a.source, v) }

// Gte compares greater than or equal within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Gte(v V) HavingPredicate[S] { return GteValue(a.source, v) }

// Contains tests a text substring within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Contains(v V) HavingPredicate[S] {
	return TextContainsValue(a.source, v)
}

// Like tests a SQL text pattern within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) Like(v V) HavingPredicate[S] { return TextLikeValue(a.source, v) }

// IContains tests a text substring without case sensitivity within the adapter's HAVING scope.
func (a TextValueFilterAdapter[S, V]) IContains(v V) HavingPredicate[S] {
	return TextIContainsValue(a.source, v)
}

// NullableTextValueFilterAdapter supplies typed HAVING operations to datatable filters.
//
// Example: expr.NullableTextValueFilter(source)
type NullableTextValueFilterAdapter[S any, V ~string] struct {
	nullableTextValueFilterPhase [0]struct{}
	_                            [0]*S
	source                       interface {
		TextValueOperand[S, V]
		NullValueOperand[S, V]
	}
}

// NullableTextValueFilter adapts a selected comparison source without changing scope, value, or phase.
//
// Example: expr.NullableTextValueFilter(source)
func NullableTextValueFilter[S any, V ~string](source interface {
	TextValueOperand[S, V]
	NullValueOperand[S, V]
}) NullableTextValueFilterAdapter[S, V] {
	return NullableTextValueFilterAdapter[S, V]{source: source}
}

// Eq compares equality within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Eq(v V) HavingPredicate[S] { return EqValue(a.source, v) }

// Ne compares inequality within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Ne(v V) HavingPredicate[S] { return NeValue(a.source, v) }

// In tests membership within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) In(vs ...V) HavingPredicate[S] {
	return InValue(a.source, vs...)
}

// Lt compares less than within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Lt(v V) HavingPredicate[S] { return LtValue(a.source, v) }

// Lte compares less than or equal within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Lte(v V) HavingPredicate[S] {
	return LteValue(a.source, v)
}

// Gt compares greater than within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Gt(v V) HavingPredicate[S] { return GtValue(a.source, v) }

// Gte compares greater than or equal within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Gte(v V) HavingPredicate[S] {
	return GteValue(a.source, v)
}

// Contains tests a text substring within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Contains(v V) HavingPredicate[S] {
	return TextContainsValue(a.source, v)
}

// Like tests a SQL text pattern within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) Like(v V) HavingPredicate[S] {
	return TextLikeValue(a.source, v)
}

// IContains tests a text substring without case sensitivity within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) IContains(v V) HavingPredicate[S] {
	return TextIContainsValue(a.source, v)
}

// IsNull tests SQL nullness within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) IsNull() HavingPredicate[S] {
	return IsNullValue(a.source)
}

// IsNotNull tests SQL presence within the adapter's HAVING scope.
func (a NullableTextValueFilterAdapter[S, V]) IsNotNull() HavingPredicate[S] {
	return IsNotNullValue(a.source)
}
