package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Interval supports ordered interval predicates and assignments, retaining calendar components.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: orm.Gt(Records.Period, temporal.Days(1)).
type Interval[M any] = ColumnHandle[M, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole]

// NullableInterval adds SQL NULL to interval predicates and assignments.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: orm.IsNull(Records.MaybePeriod).
type NullableInterval[M any] = ColumnHandle[M, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole]

// IntervalOf binds an interval without enabling numeric adjustments or text patterns.
// Example: IntervalOf(b, &m.Period).
func IntervalOf[M any](b Builder[M], ptr *temporal.Interval, opts ...ColumnOption[temporal.Interval]) Interval[M] {
	c := bindModel[M](b.state, ptr, optionCodec(codec.Interval(), opts), false, opts...)
	return Interval[M]{stored: c, input: c.assign}
}

// NullableIntervalOf binds nullable intervals. Example: NullableIntervalOf(b, &m.MaybePeriod).
func NullableIntervalOf[M any](b Builder[M], ptr *value.Nullable[temporal.Interval], opts ...ColumnOption[temporal.Interval]) NullableInterval[M] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec.Interval(), opts), opts...)
	return NullableInterval[M]{stored: c, input: c.assign}
}
