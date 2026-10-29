package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Interval supports ordered interval predicates and assignments, retaining calendar components.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: orm.IntervalGt(Records.Period, temporal.Days(1)).
type Interval[M any] struct {
	_        [0]*M
	interval columnData[temporal.Interval]
}

// NullableInterval adds SQL NULL to interval predicates and assignments.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: orm.NullableIntervalIsNull(Records.MaybePeriod).
type NullableInterval[M any] struct {
	_                [0]*M
	nullableInterval nullableColumnData[temporal.Interval]
}

// IntervalOf binds an interval without enabling numeric adjustments or text patterns.
// Example: IntervalOf(b, &m.Period).
func IntervalOf[M any](b Builder[M], ptr *temporal.Interval, opts ...ColumnOption[temporal.Interval]) Interval[M] {
	return Interval[M]{interval: bindModel[M](b.state, ptr, optionCodec(codec.Interval(), opts), false, opts...)}
}

// NullableIntervalOf binds nullable intervals. Example: NullableIntervalOf(b, &m.MaybePeriod).
func NullableIntervalOf[M any](b Builder[M], ptr *value.Nullable[temporal.Interval], opts ...ColumnOption[temporal.Interval]) NullableInterval[M] {
	return NullableInterval[M]{nullableInterval: bindNullableModel[M](b.state, ptr, optionCodec(codec.Interval(), opts), opts...)}
}

// IntervalEq compares a concrete value. Example: IntervalEq(Records.Period, period).
func IntervalEq[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Equal, []temporal.Interval{v})
}

// IntervalNe excludes a concrete value. Example: IntervalNe(Records.Period, period).
func IntervalNe[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.NotEqual, []temporal.Interval{v})
}

// IntervalIn tests membership. Example: IntervalIn(Records.Period, a, b).
func IntervalIn[M any](c Interval[M], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.In, v)
}

// IntervalNotIn excludes membership. Example: IntervalNotIn(Records.Period, a, b).
func IntervalNotIn[M any](c Interval[M], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.NotIn, v)
}

// IntervalAsc sorts ascending. Example: IntervalAsc(Records.Period).
func IntervalAsc[M any](c Interval[M]) Order[M] { return columnOrder[M](c.interval, false) }

// IntervalDesc sorts descending. Example: IntervalDesc(Records.Period).
func IntervalDesc[M any](c Interval[M]) Order[M] { return columnOrder[M](c.interval, true) }

// IntervalSet assigns a value, including zero. Example: IntervalSet(Records.Period, period).
func IntervalSet[M any](c Interval[M], v temporal.Interval) Assignment[M] {
	return columnAssignment[M](c.interval, v)
}

// IntervalGt compares ordered values. Example: IntervalGt(Records.Period, period).
func IntervalGt[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Greater, []temporal.Interval{v})
}

// IntervalGte compares ordered values. Example: IntervalGte(Records.Period, period).
func IntervalGte[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// IntervalLt compares ordered values. Example: IntervalLt(Records.Period, period).
func IntervalLt[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Less, []temporal.Interval{v})
}

// IntervalLte compares ordered values. Example: IntervalLte(Records.Period, period).
func IntervalLte[M any](c Interval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.LessOrEqual, []temporal.Interval{v})
}

// IntervalBetween includes both endpoints. Example: IntervalBetween(Records.Period, a, b).
func IntervalBetween[M any](c Interval[M], a, b temporal.Interval) Predicate[M] {
	return between[M](c.interval, a, b)
}

// NullableIntervalEq compares a concrete value. Example: NullableIntervalEq(Records.MaybePeriod, period).
func NullableIntervalEq[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Equal, []temporal.Interval{v})
}

// NullableIntervalNe excludes a concrete value. Example: NullableIntervalNe(Records.MaybePeriod, period).
func NullableIntervalNe[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.NotEqual, []temporal.Interval{v})
}

// NullableIntervalIn tests membership. Example: NullableIntervalIn(Records.MaybePeriod, a, b).
func NullableIntervalIn[M any](c NullableInterval[M], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.In, v)
}

// NullableIntervalNotIn excludes membership. Example: NullableIntervalNotIn(Records.MaybePeriod, a, b).
func NullableIntervalNotIn[M any](c NullableInterval[M], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.NotIn, v)
}

// NullableIntervalAsc sorts ascending. Example: NullableIntervalAsc(Records.MaybePeriod).
func NullableIntervalAsc[M any](c NullableInterval[M]) Order[M] {
	return columnOrder[M](c.nullableInterval.base, false)
}

// NullableIntervalDesc sorts descending. Example: NullableIntervalDesc(Records.MaybePeriod).
func NullableIntervalDesc[M any](c NullableInterval[M]) Order[M] {
	return columnOrder[M](c.nullableInterval.base, true)
}

// NullableIntervalSet assigns a value, including zero. Example: NullableIntervalSet(Records.MaybePeriod, period).
func NullableIntervalSet[M any](c NullableInterval[M], v temporal.Interval) Assignment[M] {
	return columnAssignment[M](c.nullableInterval.base, v)
}

// NullableIntervalGt compares ordered values. Example: NullableIntervalGt(Records.MaybePeriod, period).
func NullableIntervalGt[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Greater, []temporal.Interval{v})
}

// NullableIntervalGte compares ordered values. Example: NullableIntervalGte(Records.MaybePeriod, period).
func NullableIntervalGte[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// NullableIntervalLt compares ordered values. Example: NullableIntervalLt(Records.MaybePeriod, period).
func NullableIntervalLt[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Less, []temporal.Interval{v})
}

// NullableIntervalLte compares ordered values. Example: NullableIntervalLte(Records.MaybePeriod, period).
func NullableIntervalLte[M any](c NullableInterval[M], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.LessOrEqual, []temporal.Interval{v})
}

// NullableIntervalBetween includes both endpoints. Example: NullableIntervalBetween(Records.MaybePeriod, a, b).
func NullableIntervalBetween[M any](c NullableInterval[M], a, b temporal.Interval) Predicate[M] {
	return between[M](c.nullableInterval.base, a, b)
}

// NullableIntervalIsNull matches SQL NULL. Example: NullableIntervalIsNull(Records.MaybePeriod).
func NullableIntervalIsNull[M any](c NullableInterval[M]) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.IsNull, nil)
}

// NullableIntervalIsNotNull excludes SQL NULL. Example: NullableIntervalIsNotNull(Records.MaybePeriod).
func NullableIntervalIsNotNull[M any](c NullableInterval[M]) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.IsNotNull, nil)
}

// NullableIntervalSetNull explicitly clears this field. Example: NullableIntervalSetNull(Records.MaybePeriod).
func NullableIntervalSetNull[M any](c NullableInterval[M]) Assignment[M] {
	return nullAssignment[M](c.nullableInterval.base)
}
