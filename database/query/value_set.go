package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ValueSetQuery combines single-column results without erasing their value type.
// Value exposes the combined output for ordering or projection; filter each input
// with its original fields before combining when the filter belongs to that input.
type ValueSetQuery[V any] struct {
	query SetQuery[V]
	codec codec.Codec[V]
}

// Value is the combined output expression, retaining its codec and nullability.
func (q ValueSetQuery[V]) Value() Expression[Set[V], V] {
	if q.query.recordQuery().err != nil {
		return Expression[Set[V], V]{}
	}
	return Expression[Set[V], V]{node: fieldRef{setAlias, valueColumnName}, codec: q.codec}
}
func (q ValueSetQuery[V]) valueQuery() valueSubquery[V] {
	return valueSubquery[V]{subquery: q.query.subquery(), codec: q.codec}
}
func recordForValue[V any](q valueSubquery[V]) recordQuery[V] {
	return recordQuery[V]{node: q.node, err: q.err, columns: []Column{{Name: valueColumnName, Nullable: value.IsNullableType[V]()}},
		fields: []RecordField[V]{NewRecordField(valueColumnName, q.codec, func(v V) V { return v })},
		scan: func(row database.Row) (V, error) {
			var result V
			err := row.Scan(q.codec.Scan(&result))
			return result, err
		}}
}
func combineValues[V any](left, right ValueQuerySource[V], operator setOperator) ValueSetQuery[V] {
	a, b := valueSource(left), valueSource(right)
	return ValueSetQuery[V]{query: combineRecordQueries(recordForValue(a), recordForValue(b), operator), codec: a.codec}
}

// Where filters the combined value result while retaining its single-column contract.
func (q ValueSetQuery[V]) Where(predicates ...Predicate[Set[V]]) ValueSetQuery[V] {
	q.query = q.query.Where(predicates...)
	return q
}

// OrderBy orders combined values. Use q.Value().Asc() or q.Value().Desc().
func (q ValueSetQuery[V]) OrderBy(orders ...ProjectionOrder[Set[V]]) ValueSetQuery[V] {
	q.query = q.query.OrderBy(orders...)
	return q
}

// Limit bounds the combined value result.
func (q ValueSetQuery[V]) Limit(count int) ValueSetQuery[V] {
	q.query = q.query.Limit(count)
	return q
}

// Offset skips rows in the combined value result.
func (q ValueSetQuery[V]) Offset(count int) ValueSetQuery[V] {
	q.query = q.query.Offset(count)
	return q
}

// Union combines records and removes duplicate rows, retaining the single-column value contract.
func (q ValueQuery[S, V]) Union(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, unionSet)
}

// Union combines records and removes duplicate rows, retaining the single-column value contract.
func (q ValueSetQuery[V]) Union(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, unionSet)
}

// UnionAll combines records and retains duplicate rows, retaining the single-column value contract.
func (q ValueQuery[S, V]) UnionAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, unionAllSet)
}

// UnionAll combines records and retains duplicate rows, retaining the single-column value contract.
func (q ValueSetQuery[V]) UnionAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, unionAllSet)
}

// Intersect keeps distinct rows present in both inputs, retaining the single-column value contract.
func (q ValueQuery[S, V]) Intersect(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, intersectSet)
}

// Intersect keeps distinct rows present in both inputs, retaining the single-column value contract.
func (q ValueSetQuery[V]) Intersect(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, intersectSet)
}

// IntersectAll keeps shared rows with the smaller input multiplicity, retaining the single-column value contract.
func (q ValueQuery[S, V]) IntersectAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, intersectAllSet)
}

// IntersectAll keeps shared rows with the smaller input multiplicity, retaining the single-column value contract.
func (q ValueSetQuery[V]) IntersectAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, intersectAllSet)
}

// Except keeps distinct left rows absent from the right input, retaining the single-column value contract.
func (q ValueQuery[S, V]) Except(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, exceptSet)
}

// Except keeps distinct left rows absent from the right input, retaining the single-column value contract.
func (q ValueSetQuery[V]) Except(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, exceptSet)
}

// ExceptAll subtracts right-row multiplicities from the left input, retaining the single-column value contract.
func (q ValueQuery[S, V]) ExceptAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, exceptAllSet)
}

// ExceptAll subtracts right-row multiplicities from the left input, retaining the single-column value contract.
func (q ValueSetQuery[V]) ExceptAll(other ValueQuerySource[V]) ValueSetQuery[V] {
	return combineValues[V](q, other, exceptAllSet)
}

func (q ValueSetQuery[V]) recordQuery() recordQuery[V] { return q.query.recordQuery() }
func (q ValueSetQuery[V]) subquery() subquery          { return q.query.subquery() }
func (q ValueSetQuery[V]) reader() readResult[V]       { return q.query.reader() }
func (q ValueSetQuery[V]) projectionSource() projectionSource[Set[V]] {
	return q.query.projectionSource()
}
