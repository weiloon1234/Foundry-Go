package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RowValue is a sealed value evaluated before grouping/window evaluation.
// Generated fields and RowExpression implement it. Selected aggregate/window
// expressions deliberately do not, preserving the row-predicate boundary.
type RowValue[S, V any] interface{ rowValue() RowExpression[S, V] }

// RowExpression is a computed row value with concrete owner and result types.
// Value promotes it to a selected expression; Eq/Ne/In remain row predicates.
type RowExpression[S, V any] struct{ expression Expression[S, V] }

func (e RowExpression[S, V]) rowValue() RowExpression[S, V] { return e }
func (e RowExpression[S, V]) Value() Expression[S, V]       { return e.expression }
func (e RowExpression[S, V]) Asc() Order[S]                 { return rowOrder[S](e.expression.node, false) }
func (e RowExpression[S, V]) Desc() Order[S]                { return rowOrder[S](e.expression.node, true) }
func (e RowExpression[S, V]) Eq(v V) Predicate[S]           { return e.compare(equal, v) }
func (e RowExpression[S, V]) Ne(v V) Predicate[S]           { return e.compare(notEqual, v) }
func (e RowExpression[S, V]) In(v ...V) Predicate[S]        { return e.compare(in, v...) }
func (e RowExpression[S, V]) IsNull() Predicate[S]          { return e.compare(isNull) }
func (e RowExpression[S, V]) IsNotNull() Predicate[S]       { return e.compare(isNotNull) }
func (e RowExpression[S, V]) compare(op operator, v ...V) Predicate[S] {
	return Predicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, v)}
}

func rowInput[S, V any](source RowValue[S, V]) RowExpression[S, V] {
	if nilDescriptor(source) {
		return RowExpression[S, V]{}
	}
	return source.rowValue()
}
func (f valueField[M, V]) rowValue() RowExpression[M, V] { return RowExpression[M, V]{f.Value()} }
func (f NullableField[M, V]) rowValue() RowExpression[M, value.Nullable[V]] {
	return RowExpression[M, value.Nullable[V]]{f.Value()}
}

// Param captures a bound value using the field's concrete type and codec. It
// does not read the field or add a SQL column reference. On nullable fields it
// takes the same non-NULL value type as Eq; NullFor creates an explicit NULL.
func (f valueField[M, V]) Param(v V) RowExpression[M, V] { return parameterExpression[M](v, f.codec) }

// Param captures a value of this expression's exact type, including nullability.
// Encoding happens once at construction to own mutable driver buffers; an
// encoding error is retained and returned before query execution.
func (e RowExpression[S, V]) Param(v V) RowExpression[S, V] {
	return parameterExpression[S](v, e.expression.codec)
}

// Param uses this selected value's type/codec to capture a standalone bound
// value. It does not evaluate or retain the selected expression itself.
func (e Expression[S, V]) Param(v V) RowExpression[S, V] { return parameterExpression[S](v, e.codec) }
func (a Aggregate[S, V]) Param(v V) RowExpression[S, V]  { return a.Value().Param(v) }

// Param on a nullable ordered aggregate accepts its non-null comparison type,
// providing a typed fallback for CoalesceValue without losing exact decimals.
func (a NullableOrderedAggregate[S, V]) Param(v V) RowExpression[S, V] {
	return parameterExpression[S](v, a.comparisonCodec)
}

func parameterExpression[S, V any](v V, c codec.Codec[V]) RowExpression[S, V] {
	bound, err := c.Bind(v)
	return RowExpression[S, V]{Expression[S, V]{node: parameterNode{kind: c.ParameterType(), value: bound, err: err}, codec: c}}
}

// NullableRow explicitly widens a row value to one nullable layer without
// changing its SQL evaluation. Already-nullable values must not be widened again.
func NullableRow[S, V any](source RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	e := rowInput(source).Value()
	if value.IsNullableType[V]() {
		e.node = parameterNode{err: fault.New(fault.Invalid, "row value is already nullable")}
	}
	return RowExpression[S, value.Nullable[V]]{Nullable(e)}
}

// NullFor creates a typed bound SQL NULL using a non-nullable value's codec.
// The exemplar contributes its type, not a reference to its SQL value.
func NullFor[S, V any](source RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	e := NullableRow(source)
	if value.IsNullableType[V]() {
		return e
	}
	return e.Param(value.Null[V]())
}
