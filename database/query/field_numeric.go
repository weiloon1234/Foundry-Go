package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

type integerNumber interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}
type exactNumber interface {
	integerNumber | decimal.Decimal
}
type floatingNumber interface{ ~float32 | ~float64 }
type numericValue interface{ exactNumber | floatingNumber }

// ExactField permits numeric summaries for integer and exact-decimal fields.
// PostgreSQL receives a numeric input cast, avoiding integer SUM overflow.
type ExactField[M any, V exactNumber] struct{ OrderedField[M, V] }
type NullableExactField[M any, V exactNumber] struct{ NullableOrderedField[M, V] }

func NewExactField[M any, V exactNumber](table, column string, c codec.Codec[V]) ExactField[M, V] {
	return ExactField[M, V]{NewOrderedField[M, V](table, column, c)}
}
func NewNullableExactField[M any, V exactNumber](table, column string, c codec.Codec[V]) NullableExactField[M, V] {
	return NullableExactField[M, V]{NewNullableOrderedField[M, V](table, column, c)}
}

// FloatField provides approximate numeric summaries as float64. No float is
// implicitly promoted into an exact decimal result.
type FloatField[M any, V floatingNumber] struct{ OrderedField[M, V] }
type NullableFloatField[M any, V floatingNumber] struct{ NullableOrderedField[M, V] }

func NewFloatField[M any, V floatingNumber](table, column string, c codec.Codec[V]) FloatField[M, V] {
	return FloatField[M, V]{NewOrderedField[M, V](table, column, c)}
}
func NewNullableFloatField[M any, V floatingNumber](table, column string, c codec.Codec[V]) NullableFloatField[M, V] {
	return NullableFloatField[M, V]{NewNullableOrderedField[M, V](table, column, c)}
}

// Sum returns the exact sum of non-NULL values, or SQL NULL for an empty group.
func (f ExactField[M, V]) Sum() NullableOrderedAggregate[M, decimal.Decimal] {
	return fieldSummary(f.ScalarField, sumValues, exactNumberCast, codec.Decimal())
}

// Avg preserves PostgreSQL's decimal result and its finite division precision.
func (f ExactField[M, V]) Avg() NullableOrderedAggregate[M, decimal.Decimal] {
	return fieldSummary(f.ScalarField, averageValues, exactNumberCast, codec.Decimal())
}
func (f NullableExactField[M, V]) Sum() NullableOrderedAggregate[M, decimal.Decimal] {
	return fieldSummary(f.ScalarField, sumValues, exactNumberCast, codec.Decimal())
}
func (f NullableExactField[M, V]) Avg() NullableOrderedAggregate[M, decimal.Decimal] {
	return fieldSummary(f.ScalarField, averageValues, exactNumberCast, codec.Decimal())
}
func (f FloatField[M, V]) Sum() NullableOrderedAggregate[M, float64] {
	return fieldSummary(f.ScalarField, sumValues, floatNumberCast, codec.Float[float64]())
}
func (f FloatField[M, V]) Avg() NullableOrderedAggregate[M, float64] {
	return fieldSummary(f.ScalarField, averageValues, floatNumberCast, codec.Float[float64]())
}
func (f NullableFloatField[M, V]) Sum() NullableOrderedAggregate[M, float64] {
	return fieldSummary(f.ScalarField, sumValues, floatNumberCast, codec.Float[float64]())
}
func (f NullableFloatField[M, V]) Avg() NullableOrderedAggregate[M, float64] {
	return fieldSummary(f.ScalarField, averageValues, floatNumberCast, codec.Float[float64]())
}
