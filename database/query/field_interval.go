package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type intervalValue interface{ temporal.Interval }

// IntervalField adds PostgreSQL interval SUM/AVG to ordered field operations.
// Comparisons use PostgreSQL's 30-day months and 24-hour days, while returned
// intervals retain their distinct calendar and elapsed components.
type IntervalField[M any, V intervalValue] struct{ OrderedField[M, V] }
type NullableIntervalField[M any, V intervalValue] struct{ NullableOrderedField[M, V] }

func NewIntervalField[M any, V intervalValue](table, column string, c codec.Codec[V]) IntervalField[M, V] {
	return IntervalField[M, V]{NewOrderedField[M, V](table, column, c)}
}
func NewNullableIntervalField[M any, V intervalValue](table, column string, c codec.Codec[V]) NullableIntervalField[M, V] {
	return NullableIntervalField[M, V]{NewNullableOrderedField[M, V](table, column, c)}
}

// Sum retains interval components and returns NULL for an empty/all-NULL group.
func (f IntervalField[M, V]) Sum() NullableOrderedAggregate[M, V] {
	return fieldSummary(f.ScalarField, sumValues, intervalValueCast, f.codec)
}

// Avg uses PostgreSQL interval division and its whole-microsecond precision.
func (f IntervalField[M, V]) Avg() NullableOrderedAggregate[M, V] {
	return fieldSummary(f.ScalarField, averageValues, intervalValueCast, f.codec)
}
func (f NullableIntervalField[M, V]) Sum() NullableOrderedAggregate[M, V] {
	return fieldSummary(f.ScalarField, sumValues, intervalValueCast, f.codec)
}
func (f NullableIntervalField[M, V]) Avg() NullableOrderedAggregate[M, V] {
	return fieldSummary(f.ScalarField, averageValues, intervalValueCast, f.codec)
}
