package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldSource is a sealed stored field with its owner and non-null value type.
// Nullable fields retain the same non-null parameter type as Eq and Set.
// The private descriptor is data only: optional computations are instantiated
// at their call sites instead of on every generated field's method set.
// Functions retain the concrete source type to avoid boxing stored fields.
type FieldSource[S, V any] interface{ fieldData() valueField[S, V] }

func (f ScalarField[S, V]) fieldData() valueField[S, V] { return f.valueField }
func (f BinaryField[S, V]) fieldData() valueField[S, V] { return f.valueField }

type rowValueData[S, V any] Expression[S, V]

func (f ScalarField[S, V]) rowValue() rowValueData[S, V] {
	return rowValueData[S, V]{node: f.ref, codec: f.codec}
}
func (f NullableField[S, V]) rowValue() rowValueData[S, value.Nullable[V]] {
	return rowValueData[S, value.Nullable[V]]{node: f.ref, codec: codec.Nullable(f.codec)}
}
func (f BinaryField[S, V]) rowValue() rowValueData[S, V] {
	return rowValueData[S, V]{node: f.ref, codec: f.codec}
}
func (f NullableBinaryField[S, V]) rowValue() rowValueData[S, value.Nullable[V]] {
	return rowValueData[S, value.Nullable[V]]{node: f.ref, codec: codec.Nullable(f.codec)}
}

// Value selects a row value with its exact owner, value type and nullability.
// Aggregates retain their own Value method. Selected/window expressions do
// not implement RowValue, preserving the row-predicate boundary.
func Value[S, V any, F RowValue[S, V]](source F) Expression[S, V] {
	if nilDescriptor(source) {
		return Expression[S, V]{}
	}
	return Expression[S, V](source.rowValue())
}

// Param captures a bound non-null value using a stored field's concrete codec.
// It does not read the field or add a SQL column reference. Use NullFor for NULL.
// Computed expressions retain Param methods accepting their exact result type.
func Param[S, V any, F FieldSource[S, V]](source F, v V) RowExpression[S, V] {
	if nilDescriptor(source) {
		return RowExpression[S, V]{}
	}
	return parameterExpression[S](v, source.fieldData().codec)
}

// CountField counts non-NULL stored values. Count[S]() counts entire rows.
func CountField[S, V any, F FieldSource[S, V]](source F) OrderedAggregate[S, int64] {
	if nilDescriptor(source) {
		return OrderedAggregate[S, int64]{}
	}
	return countField[S](source.fieldData().ref, countValues)
}

// CountDistinctField counts distinct non-NULL stored values.
func CountDistinctField[S, V any, F FieldSource[S, V]](source F) OrderedAggregate[S, int64] {
	if nilDescriptor(source) {
		return OrderedAggregate[S, int64]{}
	}
	return countField[S](source.fieldData().ref, countDistinct)
}

// Min preserves an ordered field's exact type, returning NULL for an empty or
// all-NULL group. Unordered scalar and binary fields do not satisfy this API.
func Min[S any, V comparable, F OrderedKeyField[S, V]](source F) NullableOrderedAggregate[S, V] {
	if nilDescriptor(source) {
		return NullableOrderedAggregate[S, V]{}
	}
	return fieldExtremum(source.orderedKey(), minimumValue)
}

// Max preserves an ordered field's exact type and empty-group nullability.
func Max[S any, V comparable, F OrderedKeyField[S, V]](source F) NullableOrderedAggregate[S, V] {
	if nilDescriptor(source) {
		return NullableOrderedAggregate[S, V]{}
	}
	return fieldExtremum(source.orderedKey(), maximumValue)
}

// SummaryField is a sealed numeric or interval field carrying its exact
// aggregate result type. Integers/decimals produce decimal.Decimal, floats
// produce float64 and intervals preserve their interval type.
type SummaryField[S, R any] interface{ summaryData() summaryData[S, R] }

type summaryData[S, R any] struct {
	_     [0]*S
	field fieldRef
	cast  aggregateCast
	codec codec.Codec[R]
}

// Sum returns the sum of non-NULL values, or NULL for an empty/all-NULL group.
// Exact numeric inputs use PostgreSQL's numeric cast to avoid integer overflow.
func Sum[S, R any, F SummaryField[S, R]](source F) NullableOrderedAggregate[S, R] {
	if nilDescriptor(source) {
		return NullableOrderedAggregate[S, R]{}
	}
	return summarizeField(source.summaryData(), sumValues)
}

// Avg retains PostgreSQL's result representation and division precision.
func Avg[S, R any, F SummaryField[S, R]](source F) NullableOrderedAggregate[S, R] {
	if nilDescriptor(source) {
		return NullableOrderedAggregate[S, R]{}
	}
	return summarizeField(source.summaryData(), averageValues)
}

func summarizeField[S, R any](data summaryData[S, R], kind aggregateKind) NullableOrderedAggregate[S, R] {
	return nullableOrderedAggregate[S](aggregateNode{kind: kind, field: data.field, cast: data.cast}, data.codec)
}
