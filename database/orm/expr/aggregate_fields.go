package expr

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

// NullableOrderedAggregate retains a nullable aggregate with present-value comparisons.
// Example: expr.Sum(Orders.Total).Gt(decimal.FromInt64(1)).
type NullableOrderedAggregate[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[value.Nullable[V]]
	present       ormcore.Column[V]
}

// AggregateExpression is the reusable, sealed aggregate descriptor for relation slots.
// It retains the input scope and exact result/codec, including nullable results.
// Example: func use[S,V any](a expr.AggregateExpression[S,V]).
type AggregateExpression[S, V any] interface{ aggregateValue(*S) rowData[V] }

func (a Aggregate[S, V]) aggregateValue(*S) rowData[V]                                { return a.data }
func (a OrderedAggregate[S, V]) aggregateValue(*S) rowData[V]                         { return a.data }
func (a NullableOrderedAggregate[S, V]) aggregateValue(*S) rowData[value.Nullable[V]] { return a.data }

// FoundryAggregate supplies validated descriptor metadata and codec to relation loaders.
// Example: expr.FoundryAggregate(a,sqlowner.Expressions()).
func FoundryAggregate[S, V any](a AggregateExpression[S, V], seal sqlowner.ExpressionSeal) (ormcore.Expression, ormcore.Column[V]) {
	if !seal.Valid() || ormcore.NilDescriptor(a) {
		err := ormcore.Invalid("invalid aggregate capability or descriptor")
		return ormcore.Expression{Err: err}, ormcore.Column[V]{Err: err}
	}
	data := a.aggregateValue(nil)
	if _, ok := data.core.Node.(querycore.AggregateNode); !ok {
		data.core.Err = errors.Join(data.core.Err, ormcore.Invalid("uninitialized aggregate"))
	}
	return data.core, data.column
}

func aggregateField[S, V, R any](input ormcore.Column[V], kind querycore.AggregateKind, cast querycore.AggregateCast, result ormcore.Column[R]) rowData[R] {
	core := ormcore.Aggregate(ormcore.Expression{Node: input.Field, Refs: []ormcore.Reference{input.Reference}, Err: input.Err}, kind, cast)
	core.Err = errors.Join(core.Err, result.Err)
	return rowData[R]{core: core, column: result}
}
func scopedAggregateColumn[S, M, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) ormcore.Column[V] {
	e := At(scope, column)
	c := e.column
	c.Err = errors.Join(c.Err, e.core.Err)
	c.Reference.Scope = reflect.TypeFor[S]()
	return c
}
func nullableAggregateField[S, V any, R orderedScalar](input ormcore.Column[V], kind querycore.AggregateKind, cast querycore.AggregateCast, result ormcore.Column[R]) NullableOrderedAggregate[S, R] {
	data := nullableData(rowData[R]{column: result})
	return NullableOrderedAggregate[S, R]{data: aggregateField[S](input, kind, cast, data.column), present: result}
}

// Sum returns nullable decimal.Decimal; empty/all-NULL input is NULL.
// Example: expr.Sum(Records.Amount).
func Sum[S any, V interface{ exactNumber }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumAt returns nullable decimal.Decimal; empty/all-NULL input is NULL.
// Example: expr.SumAt(scope,Records.Amount).
func SumAt[S, M any, V interface{ exactNumber }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// Avg returns nullable decimal.Decimal; empty/all-NULL input is NULL.
// Example: expr.Avg(Records.Amount).
func Avg[S any, V interface{ exactNumber }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// AvgAt returns nullable decimal.Decimal; empty/all-NULL input is NULL.
// Example: expr.AvgAt(scope,Records.Amount).
func AvgAt[S, M any, V interface{ exactNumber }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumFloat returns nullable float64; empty/all-NULL input is NULL.
// Example: expr.SumFloat(Records.Amount).
func SumFloat[S any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, float64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumFloatAt returns nullable float64; empty/all-NULL input is NULL.
// Example: expr.SumFloatAt(scope,Records.Amount).
func SumFloatAt[S, M any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, float64] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloat returns nullable float64; empty/all-NULL input is NULL.
// Example: expr.AvgFloat(Records.Amount).
func AvgFloat[S any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, float64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloatAt returns nullable float64; empty/all-NULL input is NULL.
// Example: expr.AvgFloatAt(scope,Records.Amount).
func AvgFloatAt[S, M any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, float64] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumInterval returns nullable temporal.Interval; empty/all-NULL input is NULL.
// Example: expr.SumInterval(Records.Amount).
func SumInterval[S, P any, C orderedColumn[S, temporal.Interval, P]](column C) NullableOrderedAggregate[S, temporal.Interval] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// SumIntervalAt returns nullable temporal.Interval; empty/all-NULL input is NULL.
// Example: expr.SumIntervalAt(scope,Records.Amount).
func SumIntervalAt[S, M, P any, C orderedColumn[M, temporal.Interval, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, temporal.Interval] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgInterval returns nullable temporal.Interval; empty/all-NULL input is NULL.
// Example: expr.AvgInterval(Records.Amount).
func AvgInterval[S, P any, C orderedColumn[S, temporal.Interval, P]](column C) NullableOrderedAggregate[S, temporal.Interval] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgIntervalAt returns nullable temporal.Interval; empty/all-NULL input is NULL.
// Example: expr.AvgIntervalAt(scope,Records.Amount).
func AvgIntervalAt[S, M, P any, C orderedColumn[M, temporal.Interval, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, temporal.Interval] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// Min returns the nullable input type; empty/all-NULL input is NULL.
// Example: expr.Min(Records.Amount).
func Min[S any, V orderedScalar, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, V] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.MinimumValue, querycore.NativeNumber, c)
}

// MinAt returns the nullable input type; empty/all-NULL input is NULL.
// Example: expr.MinAt(scope,Records.Amount).
func MinAt[S, M any, V orderedScalar, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, V] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.MinimumValue, querycore.NativeNumber, c)
}

// Max returns the nullable input type; empty/all-NULL input is NULL.
// Example: expr.Max(Records.Amount).
func Max[S any, V orderedScalar, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, V] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.MaximumValue, querycore.NativeNumber, c)
}

// MaxAt returns the nullable input type; empty/all-NULL input is NULL.
// Example: expr.MaxAt(scope,Records.Amount).
func MaxAt[S, M any, V orderedScalar, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, V] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.MaximumValue, querycore.NativeNumber, c)
}

// CountField counts non-NULL stored values; empty input is zero.
// Example: expr.CountField(Records.Amount).
func CountField[S any, V, P any](column orm.ExpressionColumn[S, V, P]) OrderedAggregate[S, int64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountValues, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountFieldAt counts non-NULL stored values; empty input is zero.
// Example: expr.CountFieldAt(scope,Records.Amount).
func CountFieldAt[S, M any, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) OrderedAggregate[S, int64] {
	c := scopedAggregateColumn(scope, column)
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountValues, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountDistinctField counts non-NULL stored values; empty input is zero.
// Example: expr.CountDistinctField(Records.Amount).
func CountDistinctField[S any, V, P any](column orm.ExpressionColumn[S, V, P]) OrderedAggregate[S, int64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountDistinct, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountDistinctFieldAt counts non-NULL stored values; empty input is zero.
// Example: expr.CountDistinctFieldAt(scope,Records.Amount).
func CountDistinctFieldAt[S, M any, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) OrderedAggregate[S, int64] {
	c := scopedAggregateColumn(scope, column)
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountDistinct, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// Filter appends same-scope row predicates joined by AND, for this aggregate only.
// Example: aggregate.Filter(expr.Value(Orders.Total).Eq(1)).
func (a Aggregate[S, V]) Filter(predicates ...Predicate[S]) Aggregate[S, V] {
	a.data.core = filterAggregate(a.data.core, predicates)
	return a
}

// Asc orders selected aggregate results. Example: aggregate.Asc().
func (a Aggregate[S, V]) Asc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, false)}
}

// Desc orders selected aggregate results. Example: aggregate.Desc().
func (a Aggregate[S, V]) Desc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, true)}
}

// Filter appends same-scope row predicates joined by AND, for this aggregate only.
// Example: aggregate.Filter(expr.Value(Orders.Total).Eq(1)).
func (a OrderedAggregate[S, V]) Filter(predicates ...Predicate[S]) OrderedAggregate[S, V] {
	a.data.core = filterAggregate(a.data.core, predicates)
	return a
}

// Asc orders selected aggregate results. Example: aggregate.Asc().
func (a OrderedAggregate[S, V]) Asc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, false)}
}

// Desc orders selected aggregate results. Example: aggregate.Desc().
func (a OrderedAggregate[S, V]) Desc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, true)}
}

// Filter appends same-scope row predicates joined by AND, for this aggregate only.
// Example: aggregate.Filter(expr.Value(Orders.Total).Eq(1)).
func (a NullableOrderedAggregate[S, V]) Filter(predicates ...Predicate[S]) NullableOrderedAggregate[S, V] {
	a.data.core = filterAggregate(a.data.core, predicates)
	return a
}

// Asc orders selected aggregate results. Example: aggregate.Asc().
func (a NullableOrderedAggregate[S, V]) Asc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, false)}
}

// Desc orders selected aggregate results. Example: aggregate.Desc().
func (a NullableOrderedAggregate[S, V]) Desc() SelectedOrder[S] {
	return SelectedOrder[S]{core: ormcore.OrderExpression(a.data.core, true)}
}

func filterAggregate[S any](input ormcore.Expression, predicates []Predicate[S]) ormcore.Expression {
	if len(predicates) > 0 {
		if err := querycore.ValidateAggregateFilterCount(len(predicates)); err != nil {
			input.Err = errors.Join(input.Err, err)
			return input
		}
	}
	ps := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		ps[i] = p.core
	}
	return ormcore.FilterAggregate(input, ps)
}

// Value preserves empty-input nullability. Example: expr.Sum(Orders.Total).Value().
func (a NullableOrderedAggregate[S, V]) Value() Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: a.data}
}

// Param captures a present value for a required fallback without evaluating the aggregate.
// Example: expr.CoalesceValue(total.Value(),total.Param(decimal.FromInt64(0)).Value()).
func (a NullableOrderedAggregate[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](rowData[V]{core: a.data.core, column: a.present}, v)
}

// Eq compares groups with present values or SQL nullness. Example: aggregate.Eq(value).
func (a NullableOrderedAggregate[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares groups with present values or SQL nullness. Example: aggregate.Ne(value).
func (a NullableOrderedAggregate[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In compares groups with present values or SQL nullness. Example: aggregate.In(value).
func (a NullableOrderedAggregate[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, presentValues(vs)...)}
}

// Lt compares groups with present values or SQL nullness. Example: aggregate.Lt(value).
func (a NullableOrderedAggregate[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares groups with present values or SQL nullness. Example: aggregate.Lte(value).
func (a NullableOrderedAggregate[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares groups with present values or SQL nullness. Example: aggregate.Gt(value).
func (a NullableOrderedAggregate[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares groups with present values or SQL nullness. Example: aggregate.Gte(value).
func (a NullableOrderedAggregate[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull compares groups with present values or SQL nullness. Example: aggregate.IsNull().
func (a NullableOrderedAggregate[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull compares groups with present values or SQL nullness. Example: aggregate.IsNotNull().
func (a NullableOrderedAggregate[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}
