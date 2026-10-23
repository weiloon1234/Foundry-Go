package rows

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// Union removes duplicate rows, retaining each input window. Example: rows.Union(a,b).
func Union[R any](a, b RecordSource[R]) SetQuery[R] { return combineRecords(a, b, querycore.UnionSet) }

// UnionValues removes duplicate rows with an exact scalar result type. Example: rows.UnionValues(a,b).
func UnionValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.UnionSet)
}

// UnionAll retains duplicate rows, retaining each input window. Example: rows.UnionAll(a,b).
func UnionAll[R any](a, b RecordSource[R]) SetQuery[R] {
	return combineRecords(a, b, querycore.UnionAllSet)
}

// UnionAllValues retains duplicate rows with an exact scalar result type. Example: rows.UnionAllValues(a,b).
func UnionAllValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.UnionAllSet)
}

// Intersect keeps distinct rows present in both inputs, retaining each input window. Example: rows.Intersect(a,b).
func Intersect[R any](a, b RecordSource[R]) SetQuery[R] {
	return combineRecords(a, b, querycore.IntersectSet)
}

// IntersectValues keeps distinct rows present in both inputs with an exact scalar result type. Example: rows.IntersectValues(a,b).
func IntersectValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.IntersectSet)
}

// IntersectAll keeps the smaller shared multiplicity, retaining each input window. Example: rows.IntersectAll(a,b).
func IntersectAll[R any](a, b RecordSource[R]) SetQuery[R] {
	return combineRecords(a, b, querycore.IntersectAllSet)
}

// IntersectAllValues keeps the smaller shared multiplicity with an exact scalar result type. Example: rows.IntersectAllValues(a,b).
func IntersectAllValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.IntersectAllSet)
}

// Except keeps distinct left rows absent on the right, retaining each input window. Example: rows.Except(a,b).
func Except[R any](a, b RecordSource[R]) SetQuery[R] {
	return combineRecords(a, b, querycore.ExceptSet)
}

// ExceptValues keeps distinct left rows absent on the right with an exact scalar result type. Example: rows.ExceptValues(a,b).
func ExceptValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.ExceptSet)
}

// ExceptAll subtracts right multiplicities from the left, retaining each input window. Example: rows.ExceptAll(a,b).
func ExceptAll[R any](a, b RecordSource[R]) SetQuery[R] {
	return combineRecords(a, b, querycore.ExceptAllSet)
}

// ExceptAllValues subtracts right multiplicities from the left with an exact scalar result type. Example: rows.ExceptAllValues(a,b).
func ExceptAllValues[V any](a, b ValueSource[V]) ValueSetQuery[V] {
	return combineValues(a, b, querycore.ExceptAllSet)
}
