package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Interval supports ordered interval predicates and assignments, retaining calendar components.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: Records.Period.Gt(temporal.Days(1)).
type Interval[M any] struct {
	_        [0]*M
	interval columnData[temporal.Interval]
}

// NullableInterval adds SQL NULL to interval predicates and assignments.
// PostgreSQL compares using 30-day months and 24-hour days: Eq(temporal.Months(1))
// also matches 30 days. Hydration preserves distinct calendar and elapsed components.
// Example: Records.MaybePeriod.IsNull().
type NullableInterval[M any] struct {
	_                [0]*M
	nullableInterval nullableColumnData[temporal.Interval]
}

// IntervalOf binds an interval without enabling numeric adjustments or text patterns.
// Example: IntervalOf(b, &m.Period).
func IntervalOf[M any](b Builder[M], ptr *temporal.Interval) Interval[M] {
	return Interval[M]{interval: bindTyped(b.state, ptr, codec.Interval(), false)}
}

// NullableIntervalOf binds nullable intervals. Example: NullableIntervalOf(b, &m.MaybePeriod).
func NullableIntervalOf[M any](b Builder[M], ptr *value.Nullable[temporal.Interval]) NullableInterval[M] {
	return NullableInterval[M]{nullableInterval: bindNullable(b.state, ptr, codec.Interval())}
}

// Eq compares a concrete value. Example: Records.Period.Eq(period).
func (c Interval[M]) Eq(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Equal, []temporal.Interval{v})
}

// Ne excludes a concrete value. Example: Records.Period.Ne(period).
func (c Interval[M]) Ne(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.NotEqual, []temporal.Interval{v})
}

// In tests membership. Example: Records.Period.In(a, b).
func (c Interval[M]) In(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.In, v)
}

// NotIn excludes membership. Example: Records.Period.NotIn(a, b).
func (c Interval[M]) NotIn(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Records.Period.Asc().
func (c Interval[M]) Asc() Order[M] { return columnOrder[M](c.interval, false) }

// Desc sorts descending. Example: Records.Period.Desc().
func (c Interval[M]) Desc() Order[M] { return columnOrder[M](c.interval, true) }

// Set assigns a value, including zero. Example: Records.Period.Set(period).
func (c Interval[M]) Set(v temporal.Interval) Assignment[M] {
	return columnAssignment[M](c.interval, v)
}

// Gt compares ordered values. Example: Records.Period.Gt(period).
func (c Interval[M]) Gt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Greater, []temporal.Interval{v})
}

// Gte compares ordered values. Example: Records.Period.Gte(period).
func (c Interval[M]) Gte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// Lt compares ordered values. Example: Records.Period.Lt(period).
func (c Interval[M]) Lt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.Less, []temporal.Interval{v})
}

// Lte compares ordered values. Example: Records.Period.Lte(period).
func (c Interval[M]) Lte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.interval, querycore.LessOrEqual, []temporal.Interval{v})
}

// Between includes both endpoints. Example: Records.Period.Between(a, b).
func (c Interval[M]) Between(a, b temporal.Interval) Predicate[M] {
	return between[M](c.interval, a, b)
}

func (c Interval[M]) ormColumn(*M) columnData[temporal.Interval] { return c.interval }
func (c Interval[M]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[temporal.Interval]) {
	return requiredExpression(c.interval)
}

// Eq compares a concrete value. Example: Records.MaybePeriod.Eq(period).
func (c NullableInterval[M]) Eq(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Equal, []temporal.Interval{v})
}

// Ne excludes a concrete value. Example: Records.MaybePeriod.Ne(period).
func (c NullableInterval[M]) Ne(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.NotEqual, []temporal.Interval{v})
}

// In tests membership. Example: Records.MaybePeriod.In(a, b).
func (c NullableInterval[M]) In(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.In, v)
}

// NotIn excludes membership. Example: Records.MaybePeriod.NotIn(a, b).
func (c NullableInterval[M]) NotIn(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Records.MaybePeriod.Asc().
func (c NullableInterval[M]) Asc() Order[M] { return columnOrder[M](c.nullableInterval.base, false) }

// Desc sorts descending. Example: Records.MaybePeriod.Desc().
func (c NullableInterval[M]) Desc() Order[M] { return columnOrder[M](c.nullableInterval.base, true) }

// Set assigns a value, including zero. Example: Records.MaybePeriod.Set(period).
func (c NullableInterval[M]) Set(v temporal.Interval) Assignment[M] {
	return columnAssignment[M](c.nullableInterval.base, v)
}

// Gt compares ordered values. Example: Records.MaybePeriod.Gt(period).
func (c NullableInterval[M]) Gt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Greater, []temporal.Interval{v})
}

// Gte compares ordered values. Example: Records.MaybePeriod.Gte(period).
func (c NullableInterval[M]) Gte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// Lt compares ordered values. Example: Records.MaybePeriod.Lt(period).
func (c NullableInterval[M]) Lt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.Less, []temporal.Interval{v})
}

// Lte compares ordered values. Example: Records.MaybePeriod.Lte(period).
func (c NullableInterval[M]) Lte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.LessOrEqual, []temporal.Interval{v})
}

// Between includes both endpoints. Example: Records.MaybePeriod.Between(a, b).
func (c NullableInterval[M]) Between(a, b temporal.Interval) Predicate[M] {
	return between[M](c.nullableInterval.base, a, b)
}

// IsNull matches SQL NULL. Example: Records.MaybePeriod.IsNull().
func (c NullableInterval[M]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Records.MaybePeriod.IsNotNull().
func (c NullableInterval[M]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInterval.base, querycore.IsNotNull, nil)
}

// SetNull explicitly clears this field. Example: Records.MaybePeriod.SetNull().
func (c NullableInterval[M]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInterval.base)
}

func (c NullableInterval[M]) ormColumn(*M) columnData[temporal.Interval] {
	return c.nullableInterval.base
}
func (c NullableInterval[M]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[value.Nullable[temporal.Interval]]) {
	return nullableExpression(c.nullableInterval)
}
