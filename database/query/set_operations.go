package query

// Union combines records and removes duplicate rows. Both inputs must return the same complete record type.
func Union[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, unionSet)
}

// Union combines records and removes duplicate rows; input filters and windows are preserved.
func (q Query[M]) Union(other RecordQuerySource[M]) SetQuery[M] { return Union(q, other) }

// Union combines records and removes duplicate rows; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) Union(other RecordQuerySource[P]) SetQuery[P] { return Union(q, other) }

// Union combines records and removes duplicate rows; input filters and windows are preserved.
func (q SetQuery[R]) Union(other RecordQuerySource[R]) SetQuery[R] { return Union(q, other) }

// UnionAll combines records and retains duplicate rows. Both inputs must return the same complete record type.
func UnionAll[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, unionAllSet)
}

// UnionAll combines records and retains duplicate rows; input filters and windows are preserved.
func (q Query[M]) UnionAll(other RecordQuerySource[M]) SetQuery[M] { return UnionAll(q, other) }

// UnionAll combines records and retains duplicate rows; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) UnionAll(other RecordQuerySource[P]) SetQuery[P] {
	return UnionAll(q, other)
}

// UnionAll combines records and retains duplicate rows; input filters and windows are preserved.
func (q SetQuery[R]) UnionAll(other RecordQuerySource[R]) SetQuery[R] { return UnionAll(q, other) }

// Intersect keeps distinct rows present in both inputs. Both inputs must return the same complete record type.
func Intersect[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, intersectSet)
}

// Intersect keeps distinct rows present in both inputs; input filters and windows are preserved.
func (q Query[M]) Intersect(other RecordQuerySource[M]) SetQuery[M] { return Intersect(q, other) }

// Intersect keeps distinct rows present in both inputs; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) Intersect(other RecordQuerySource[P]) SetQuery[P] {
	return Intersect(q, other)
}

// Intersect keeps distinct rows present in both inputs; input filters and windows are preserved.
func (q SetQuery[R]) Intersect(other RecordQuerySource[R]) SetQuery[R] { return Intersect(q, other) }

// IntersectAll keeps shared rows with the smaller input multiplicity. Both inputs must return the same complete record type.
func IntersectAll[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, intersectAllSet)
}

// IntersectAll keeps shared rows with the smaller input multiplicity; input filters and windows are preserved.
func (q Query[M]) IntersectAll(other RecordQuerySource[M]) SetQuery[M] { return IntersectAll(q, other) }

// IntersectAll keeps shared rows with the smaller input multiplicity; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) IntersectAll(other RecordQuerySource[P]) SetQuery[P] {
	return IntersectAll(q, other)
}

// IntersectAll keeps shared rows with the smaller input multiplicity; input filters and windows are preserved.
func (q SetQuery[R]) IntersectAll(other RecordQuerySource[R]) SetQuery[R] {
	return IntersectAll(q, other)
}

// Except keeps distinct left rows absent from the right input. Both inputs must return the same complete record type.
func Except[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, exceptSet)
}

// Except keeps distinct left rows absent from the right input; input filters and windows are preserved.
func (q Query[M]) Except(other RecordQuerySource[M]) SetQuery[M] { return Except(q, other) }

// Except keeps distinct left rows absent from the right input; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) Except(other RecordQuerySource[P]) SetQuery[P] {
	return Except(q, other)
}

// Except keeps distinct left rows absent from the right input; input filters and windows are preserved.
func (q SetQuery[R]) Except(other RecordQuerySource[R]) SetQuery[R] { return Except(q, other) }

// ExceptAll subtracts right-row multiplicities from the left input. Both inputs must return the same complete record type.
func ExceptAll[R any](left, right RecordQuerySource[R]) SetQuery[R] {
	return combineRecords(left, right, exceptAllSet)
}

// ExceptAll subtracts right-row multiplicities from the left input; input filters and windows are preserved.
func (q Query[M]) ExceptAll(other RecordQuerySource[M]) SetQuery[M] { return ExceptAll(q, other) }

// ExceptAll subtracts right-row multiplicities from the left input; input filters and windows are preserved.
func (q ProjectionQuery[S, P]) ExceptAll(other RecordQuerySource[P]) SetQuery[P] {
	return ExceptAll(q, other)
}

// ExceptAll subtracts right-row multiplicities from the left input; input filters and windows are preserved.
func (q SetQuery[R]) ExceptAll(other RecordQuerySource[R]) SetQuery[R] { return ExceptAll(q, other) }
