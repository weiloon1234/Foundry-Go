package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

// NullableOrderedAggregate retains a nullable aggregate with present-value comparisons.
//
// Example: expr.GtValue(expr.Sum(orm.NumberExpr(Orders.Total)), decimal.FromInt64(1))
type NullableOrderedAggregate[S any, V orderedScalar] = nullableOrderedAggregate[*S, V]

type nullableOrderedAggregate[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[value.Nullable[V]]
	present       ormcore.Column[V]
}

// AggregateExpression is the reusable, sealed aggregate descriptor for relation slots.
// It retains the input scope and exact result/codec, including nullable results.
//
// Example: var aggregate expr.AggregateExpression[User, int64] = expr.Count[User]()
type AggregateExpression[S, V any] interface{ aggregateValue(*S) rowData[V] }

func (a aggregate[S, V]) aggregateValue(S) rowData[V]                                { return a.data }
func (a orderedAggregate[S, V]) aggregateValue(S) rowData[V]                         { return a.data }
func (a nullableOrderedAggregate[S, V]) aggregateValue(S) rowData[value.Nullable[V]] { return a.data }

// FoundryAggregate supplies validated descriptor metadata and codec to relation loaders.
//
// Example: expr.FoundryAggregate(a, seal)
func FoundryAggregate[S, V any](a AggregateExpression[S, V], seal sqlowner.ExpressionSeal) (ormcore.Expression, ormcore.Column[V]) {
	if !seal.Valid() || ormcore.NilDescriptor(a) {
		err := ormcore.Invalid("invalid aggregate capability or descriptor")
		return ormcore.Expression{Err: err}, ormcore.Column[V]{Err: err}
	}
	data := a.aggregateValue(nil)
	if _, ok := data.core.Node.(querycore.AggregateNode); !ok {
		data.core.Err = safeerror.Join("operation failed", data.core.Err, ormcore.Invalid("uninitialized aggregate"))
	}
	return data.core, data.column
}

func aggregateField[S, V, R any](input ormcore.Column[V], kind querycore.AggregateKind, cast querycore.AggregateCast, result ormcore.Column[R]) rowData[R] {
	core := ormcore.Aggregate(ormcore.Expression{Sensitive: input.Sensitive, Node: input.Field, Refs: []ormcore.Reference{input.Reference}, Err: input.Err}, kind, cast)
	core.Err = safeerror.Join("operation failed", core.Err, result.Err)
	return rowData[R]{core: core, column: result}
}
func scopedAggregateColumn[S, M, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) ormcore.Column[V] {
	e := At(scope, column)
	c := e.column
	c.Err = safeerror.Join("operation failed", c.Err, e.core.Err)
	c.Reference.Scope = reflect.TypeFor[S]()
	return c
}
func nullableAggregateField[S, V any, R orderedScalar](input ormcore.Column[V], kind querycore.AggregateKind, cast querycore.AggregateCast, result ormcore.Column[R]) NullableOrderedAggregate[S, R] {
	data := nullableData(rowData[R]{column: result})
	return NullableOrderedAggregate[S, R]{data: aggregateField[S](input, kind, cast, data.column), present: result}
}

// Sum returns nullable decimal.Decimal; empty/all-NULL input is NULL.
//
// Example: expr.Sum(column)
func Sum[S any, V interface{ exactNumber }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumAt returns nullable decimal.Decimal; empty/all-NULL input is NULL.
//
// Example: expr.SumAt(scope, column)
func SumAt[S, M any, V interface{ exactNumber }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// Avg returns nullable decimal.Decimal; empty/all-NULL input is NULL.
//
// Example: expr.Avg(column)
func Avg[S any, V interface{ exactNumber }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// AvgAt returns nullable decimal.Decimal; empty/all-NULL input is NULL.
//
// Example: expr.AvgAt(scope, column)
func AvgAt[S, M any, V interface{ exactNumber }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, decimal.Decimal] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumFloat returns nullable float64; empty/all-NULL input is NULL.
//
// Example: expr.SumFloat(column)
func SumFloat[S any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, float64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumFloatAt returns nullable float64; empty/all-NULL input is NULL.
//
// Example: expr.SumFloatAt(scope, column)
func SumFloatAt[S, M any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, float64] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloat returns nullable float64; empty/all-NULL input is NULL.
//
// Example: expr.AvgFloat(column)
func AvgFloat[S any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, float64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloatAt returns nullable float64; empty/all-NULL input is NULL.
//
// Example: expr.AvgFloatAt(scope, column)
func AvgFloatAt[S, M any, V interface{ ~float32 | ~float64 }, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, float64] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumInterval returns nullable temporal.Interval; empty/all-NULL input is NULL.
//
// Example: expr.SumInterval(column)
func SumInterval[S, P any, C orderedColumn[S, temporal.Interval, P]](column C) NullableOrderedAggregate[S, temporal.Interval] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// SumIntervalAt returns nullable temporal.Interval; empty/all-NULL input is NULL.
//
// Example: expr.SumIntervalAt(scope, column)
func SumIntervalAt[S, M, P any, C orderedColumn[M, temporal.Interval, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, temporal.Interval] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgInterval returns nullable temporal.Interval; empty/all-NULL input is NULL.
//
// Example: expr.AvgInterval(column)
func AvgInterval[S, P any, C orderedColumn[S, temporal.Interval, P]](column C) NullableOrderedAggregate[S, temporal.Interval] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgIntervalAt returns nullable temporal.Interval; empty/all-NULL input is NULL.
//
// Example: expr.AvgIntervalAt(scope, column)
func AvgIntervalAt[S, M, P any, C orderedColumn[M, temporal.Interval, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, temporal.Interval] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// Min returns the nullable input type; empty/all-NULL input is NULL.
//
// Example: expr.Min(column)
func Min[S any, V orderedScalar, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, V] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.MinimumValue, querycore.NativeNumber, c)
}

// MinAt returns the nullable input type; empty/all-NULL input is NULL.
//
// Example: expr.MinAt(scope, column)
func MinAt[S, M any, V orderedScalar, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, V] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.MinimumValue, querycore.NativeNumber, c)
}

// Max returns the nullable input type; empty/all-NULL input is NULL.
//
// Example: expr.Max(column)
func Max[S any, V orderedScalar, P any, C orderedColumn[S, V, P]](column C) NullableOrderedAggregate[S, V] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return nullableAggregateField[S](c, querycore.MaximumValue, querycore.NativeNumber, c)
}

// MaxAt returns the nullable input type; empty/all-NULL input is NULL.
//
// Example: expr.MaxAt(scope, column)
func MaxAt[S, M any, V orderedScalar, P any, C orderedColumn[M, V, P]](scope Scope[S, M], column C) NullableOrderedAggregate[S, V] {
	c := scopedAggregateColumn(scope, column)
	return nullableAggregateField[S](c, querycore.MaximumValue, querycore.NativeNumber, c)
}

// CountField counts non-NULL stored values; empty input is zero.
//
// Example: expr.CountField(column)
func CountField[S any, V, P any](column orm.ExpressionColumn[S, V, P]) OrderedAggregate[S, int64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountValues, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountFieldAt counts non-NULL stored values; empty input is zero.
//
// Example: expr.CountFieldAt(scope, column)
func CountFieldAt[S, M any, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) OrderedAggregate[S, int64] {
	c := scopedAggregateColumn(scope, column)
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountValues, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountDistinctField counts non-NULL stored values; empty input is zero.
//
// Example: expr.CountDistinctField(column)
func CountDistinctField[S any, V, P any](column orm.ExpressionColumn[S, V, P]) OrderedAggregate[S, int64] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountDistinct, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

// CountDistinctFieldAt counts non-NULL stored values; empty input is zero.
//
// Example: expr.CountDistinctFieldAt(scope, column)
func CountDistinctFieldAt[S, M any, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) OrderedAggregate[S, int64] {
	c := scopedAggregateColumn(scope, column)
	return OrderedAggregate[S, int64]{data: aggregateField[S](c, querycore.CountDistinct, querycore.NativeNumber, columnCodec(codec.Signed[int64]()))}
}

func filterAggregatePointer[S any](input ormcore.Expression, predicates []predicate[S]) ormcore.Expression {
	if len(predicates) > 0 {
		if err := querycore.ValidateAggregateFilterCount(len(predicates)); err != nil {
			input.Err = safeerror.Join("operation failed", input.Err, err)
			return input
		}
	}
	ps := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		ps[i] = p.core
	}
	return ormcore.FilterAggregate(input, ps)
}
