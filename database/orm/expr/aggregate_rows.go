package expr

import (
	"database/sql"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// A concrete RowExpression prevents donor embeddings from supplying capabilities.
// The field's own metadata, not its Go underlying type, authorizes aggregation.
func aggregateRowData[S, V any](input RowExpression[S, V], kind querycore.AggregateKind, cast querycore.AggregateCast, ordered bool) rowData[V] {
	data := input.data
	data.core.Err = safeerror.Join("operation failed", data.core.Err, data.column.Err)
	if ordered && !data.column.AggregateInput {
		data.core.Err = safeerror.Join("operation failed", data.core.Err, ormcore.Invalid("aggregate requires an ordered declared result field"))
	}
	data.core.Sensitive = data.core.Sensitive || data.column.Sensitive
	data.core = ormcore.Aggregate(data.core, kind, cast)
	return data
}

func nullableRowAggregate[S, V any, R orderedScalar](input RowExpression[S, V], kind querycore.AggregateKind, cast querycore.AggregateCast, result ormcore.Column[R]) NullableOrderedAggregate[S, R] {
	data := aggregateRowData(input, kind, cast, true)
	output := nullableData(rowData[R]{core: data.core, column: result})
	return NullableOrderedAggregate[S, R]{data: output, present: result}
}

// SumRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumRow(rows.At(scope, Report.Amount)).
func SumRow[S any, V exactNumber](input RowExpression[S, V]) NullableOrderedAggregate[S, decimal.Decimal] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumNullableRow(rows.At(scope, Report.Amount)).
func SumNullableRow[S any, V exactNumber](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, decimal.Decimal] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// AvgRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgRow(rows.At(scope, Report.Amount)).
func AvgRow[S any, V exactNumber](input RowExpression[S, V]) NullableOrderedAggregate[S, decimal.Decimal] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// AvgNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgNullableRow(rows.At(scope, Report.Amount)).
func AvgNullableRow[S any, V exactNumber](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, decimal.Decimal] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.ExactNumberCast, columnCodec(codec.Decimal()))
}

// SumFloatRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumFloatRow(rows.At(scope, Report.Amount)).
func SumFloatRow[S any, V interface{ ~float32 | ~float64 }](input RowExpression[S, V]) NullableOrderedAggregate[S, float64] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumFloatNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumFloatNullableRow(rows.At(scope, Report.Amount)).
func SumFloatNullableRow[S any, V interface{ ~float32 | ~float64 }](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, float64] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloatRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgFloatRow(rows.At(scope, Report.Amount)).
func AvgFloatRow[S any, V interface{ ~float32 | ~float64 }](input RowExpression[S, V]) NullableOrderedAggregate[S, float64] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// AvgFloatNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgFloatNullableRow(rows.At(scope, Report.Amount)).
func AvgFloatNullableRow[S any, V interface{ ~float32 | ~float64 }](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, float64] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.FloatNumberCast, columnCodec(codec.Float[float64]()))
}

// SumIntervalRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumIntervalRow(rows.At(scope, Report.Amount)).
func SumIntervalRow[S any, V temporal.Interval](input RowExpression[S, V]) NullableOrderedAggregate[S, temporal.Interval] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// SumIntervalNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.SumIntervalNullableRow(rows.At(scope, Report.Amount)).
func SumIntervalNullableRow[S any, V temporal.Interval](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, temporal.Interval] {
	return nullableRowAggregate(input, querycore.SumValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgIntervalRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgIntervalRow(rows.At(scope, Report.Amount)).
func AvgIntervalRow[S any, V temporal.Interval](input RowExpression[S, V]) NullableOrderedAggregate[S, temporal.Interval] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// AvgIntervalNullableRow aggregates a declared derived field; empty/all-NULL input remains NULL.
// Example: expr.AvgIntervalNullableRow(rows.At(scope, Report.Amount)).
func AvgIntervalNullableRow[S any, V temporal.Interval](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, temporal.Interval] {
	return nullableRowAggregate(input, querycore.AverageValues, querycore.IntervalValueCast, columnCodec(codec.Interval()))
}

// MinRow retains the input codec with exactly one nullable layer.
// Example: expr.MinRow(rows.At(scope, Report.Amount)).
func MinRow[S any, V orderedScalar](input RowExpression[S, V]) NullableOrderedAggregate[S, V] {
	return nullableRowAggregate(input, querycore.MinimumValue, querycore.NativeNumber, input.data.column)
}

// MinNullableRow retains the input codec with exactly one nullable layer.
// Example: expr.MinNullableRow(rows.At(scope, Report.Amount)).
func MinNullableRow[S any, V orderedScalar](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, V] {
	data := aggregateRowData(input, querycore.MinimumValue, querycore.NativeNumber, true)
	return NullableOrderedAggregate[S, V]{data: data, present: presentAggregateColumn(data.column)}
}

// MaxRow retains the input codec with exactly one nullable layer.
// Example: expr.MaxRow(rows.At(scope, Report.Amount)).
func MaxRow[S any, V orderedScalar](input RowExpression[S, V]) NullableOrderedAggregate[S, V] {
	return nullableRowAggregate(input, querycore.MaximumValue, querycore.NativeNumber, input.data.column)
}

// MaxNullableRow retains the input codec with exactly one nullable layer.
// Example: expr.MaxNullableRow(rows.At(scope, Report.Amount)).
func MaxNullableRow[S any, V orderedScalar](input RowExpression[S, value.Nullable[V]]) NullableOrderedAggregate[S, V] {
	data := aggregateRowData(input, querycore.MaximumValue, querycore.NativeNumber, true)
	return NullableOrderedAggregate[S, V]{data: data, present: presentAggregateColumn(data.column)}
}

// CountRow counts non-NULL derived field values, returning zero for empty input.
// Example: expr.CountRow(rows.At(scope, Report.Amount)).
func CountRow[S, V any](input RowExpression[S, V]) OrderedAggregate[S, int64] {
	data := aggregateRowData(input, querycore.CountValues, querycore.NativeNumber, false)
	return OrderedAggregate[S, int64]{data: rowData[int64]{core: data.core, column: columnCodec(codec.Signed[int64]())}}
}

// CountDistinctRow counts non-NULL derived field values, returning zero for empty input.
// Example: expr.CountDistinctRow(rows.At(scope, Report.Amount)).
func CountDistinctRow[S, V any](input RowExpression[S, V]) OrderedAggregate[S, int64] {
	data := aggregateRowData(input, querycore.CountDistinct, querycore.NativeNumber, false)
	return OrderedAggregate[S, int64]{data: rowData[int64]{core: data.core, column: columnCodec(codec.Signed[int64]())}}
}

// Preserve a nullable input's custom codec for MIN/MAX and present fallback Params.
func presentAggregateColumn[V any](c ormcore.Column[value.Nullable[V]]) ormcore.Column[V] {
	out := ormcore.Column[V]{Text: c.Text, Kind: c.Kind, Sensitive: c.Sensitive, Err: c.Err}
	out.Bind = func(v V) (driver.Value, error) {
		if c.Bind == nil {
			return nil, ormcore.Invalid("uninitialized aggregate codec")
		}
		return c.Bind(value.Of(v))
	}
	out.Clone = func(v V) V {
		if c.Clone == nil {
			return v
		}
		result, _ := c.Clone(value.Of(v)).Get()
		return result
	}
	out.Scan = func(dst *V) any {
		return aggregateScanner(func(src any) error {
			if c.Scan == nil || dst == nil {
				return ormcore.Invalid("uninitialized aggregate codec")
			}
			var item value.Nullable[V]
			scanner, ok := c.Scan(&item).(sql.Scanner)
			if !ok {
				return ormcore.Invalid("invalid aggregate scanner")
			}
			if err := scanner.Scan(src); err != nil {
				return err
			}
			result, ok := item.Get()
			if !ok {
				return ormcore.Invalid("required aggregate fallback is NULL")
			}
			*dst = result
			return nil
		})
	}
	return out
}

type aggregateScanner func(any) error

func (scan aggregateScanner) Scan(src any) error { return scan(src) }
