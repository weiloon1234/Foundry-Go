package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AddIntervals adds interval components without conflating calendar and elapsed units.
func AddIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(AddIntervalsValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddIntervalsValue retains selected aggregate/window phases and input ownership.
func AddIntervalsValue[S any](left Selected[S, temporal.Interval], right Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.AddIntervalsOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// AddIntervalsNullable adds interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
func AddIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(AddIntervalsNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddIntervalsNullableValue retains selected aggregate/window phases and input ownership.
func AddIntervalsNullableValue[S any](left Selected[S, value.Nullable[temporal.Interval]], right Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.AddIntervalsOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// SubtractIntervals subtracts interval components without conflating calendar and elapsed units.
func SubtractIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(SubtractIntervalsValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractIntervalsValue retains selected aggregate/window phases and input ownership.
func SubtractIntervalsValue[S any](left Selected[S, temporal.Interval], right Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.SubtractIntervalsOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// SubtractIntervalsNullable subtracts interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
func SubtractIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(SubtractIntervalsNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractIntervalsNullableValue retains selected aggregate/window phases and input ownership.
func SubtractIntervalsNullableValue[S any](left Selected[S, value.Nullable[temporal.Interval]], right Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.SubtractIntervalsOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// NegateInterval negates all interval components; database overflow remains an execution error.
func NegateInterval[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(NegateIntervalValue(rowInput(input).Value()))
}

// NegateIntervalValue retains selected aggregate/window phases and input ownership.
func NegateIntervalValue[S any](input Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.NegateIntervalOperation, columnCodec(codec.Interval()), operationArg(input))
}

// NegateIntervalNullable negates all interval components; database overflow remains an execution error. SQL NULL propagates; widen non-null inputs explicitly.
func NegateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(NegateIntervalNullableValue(rowInput(input).Value()))
}

// NegateIntervalNullableValue retains selected aggregate/window phases and input ownership.
func NegateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.NegateIntervalOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(input))
}

// LocalDifference subtracts local date-times using PostgreSQL's normalized interval result.
func LocalDifference[S any](left RowValue[S, temporal.LocalDateTime], right RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(LocalDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// LocalDifferenceValue retains selected aggregate/window phases and input ownership.
func LocalDifferenceValue[S any](left Selected[S, temporal.LocalDateTime], right Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.LocalDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// LocalDifferenceNullable subtracts local date-times using PostgreSQL's normalized interval result. SQL NULL propagates; widen non-null inputs explicitly.
func LocalDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.LocalDateTime]], right RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(LocalDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// LocalDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func LocalDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.LocalDateTime]], right Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.LocalDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// InstantDifference subtracts instants using PostgreSQL's 24-hour-day interval normalization.
func InstantDifference[S any, L, R instantValue](left RowValue[S, L], right RowValue[S, R]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(InstantDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// InstantDifferenceValue retains selected aggregate/window phases and input ownership.
func InstantDifferenceValue[S any, L, R instantValue](left Selected[S, L], right Selected[S, R]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.InstantDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// InstantDifferenceNullable subtracts instants using PostgreSQL's 24-hour-day interval normalization. SQL NULL propagates; widen non-null inputs explicitly.
func InstantDifferenceNullable[S any, L, R instantValue](left RowValue[S, value.Nullable[L]], right RowValue[S, value.Nullable[R]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(InstantDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// InstantDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func InstantDifferenceNullableValue[S any, L, R instantValue](left Selected[S, value.Nullable[L]], right Selected[S, value.Nullable[R]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.InstantDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// ClockDifference subtracts clock components without wrapping the signed result at midnight.
func ClockDifference[S any](left RowValue[S, temporal.Time], right RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(ClockDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// ClockDifferenceValue retains selected aggregate/window phases and input ownership.
func ClockDifferenceValue[S any](left Selected[S, temporal.Time], right Selected[S, temporal.Time]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.ClockDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// ClockDifferenceNullable subtracts clock components without wrapping the signed result at midnight. SQL NULL propagates; widen non-null inputs explicitly.
func ClockDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Time]], right RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(ClockDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// ClockDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func ClockDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.Time]], right Selected[S, value.Nullable[temporal.Time]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.ClockDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// IntervalMonths returns the complete signed month component, including whole years.
func IntervalMonths[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalMonthsValue(rowInput(input).Value()))
}

// IntervalMonthsValue retains selected aggregate/window phases and input ownership.
func IntervalMonthsValue[S any](input Selected[S, temporal.Interval]) Selected[S, int32] {
	return operationValue[S](querycore.IntervalMonthsOperation, columnCodec(codec.Signed[int32]()), operationArg(input))
}

// IntervalMonthsNullable returns the complete signed month component, including whole years. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalMonthsNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalMonthsNullableValue(rowInput(input).Value()))
}

// IntervalMonthsNullableValue retains selected aggregate/window phases and input ownership.
func IntervalMonthsNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[int32]] {
	return operationValue[S](querycore.IntervalMonthsOperation, columnCodec(codec.Nullable(codec.Signed[int32]())), operationArg(input))
}

// IntervalDays returns the signed calendar-day component.
func IntervalDays[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalDaysValue(rowInput(input).Value()))
}

// IntervalDaysValue retains selected aggregate/window phases and input ownership.
func IntervalDaysValue[S any](input Selected[S, temporal.Interval]) Selected[S, int32] {
	return operationValue[S](querycore.IntervalDaysOperation, columnCodec(codec.Signed[int32]()), operationArg(input))
}

// IntervalDaysNullable returns the signed calendar-day component. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalDaysNullableValue(rowInput(input).Value()))
}

// IntervalDaysNullableValue retains selected aggregate/window phases and input ownership.
func IntervalDaysNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[int32]] {
	return operationValue[S](querycore.IntervalDaysOperation, columnCodec(codec.Nullable(codec.Signed[int32]())), operationArg(input))
}

// IntervalElapsed returns the elapsed component as a Go duration, excluding months and days.
func IntervalElapsed[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, time.Duration] {
	return orderedRow(IntervalElapsedValue(rowInput(input).Value()))
}

// IntervalElapsedValue retains selected aggregate/window phases and input ownership.
func IntervalElapsedValue[S any](input Selected[S, temporal.Interval]) Selected[S, time.Duration] {
	return operationValue[S](querycore.IntervalElapsedOperation, columnCodec(codec.Signed[time.Duration]()), operationArg(input))
}

// IntervalElapsedNullable returns the elapsed component as a Go duration, excluding months and days. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, time.Duration] {
	return nullableOrderedRow(IntervalElapsedNullableValue(rowInput(input).Value()))
}

// IntervalElapsedNullableValue retains selected aggregate/window phases and input ownership.
func IntervalElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[time.Duration]] {
	return operationValue[S](querycore.IntervalElapsedOperation, columnCodec(codec.Nullable(codec.Signed[time.Duration]())), operationArg(input))
}

// ShiftDate adds a typed interval expression to a date, returning local date-time components.
func ShiftDate[S any](input RowValue[S, temporal.Date], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftDateValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftDateValue retains selected aggregate/window phases and input ownership.
func ShiftDateValue[S any](input Selected[S, temporal.Date], interval Selected[S, temporal.Interval]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftDateNullable adds a typed interval expression to a date, returning local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftDateNullableValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftDateNullableValue retains selected aggregate/window phases and input ownership.
func ShiftDateNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), operationArg(interval))
}

// ShiftLocal adds a typed interval expression to local date-time components.
func ShiftLocal[S any](input RowValue[S, temporal.LocalDateTime], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftLocalValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftLocalValue retains selected aggregate/window phases and input ownership.
func ShiftLocalValue[S any](input Selected[S, temporal.LocalDateTime], interval Selected[S, temporal.Interval]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftLocalNullable adds a typed interval expression to local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftLocalNullableValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftLocalNullableValue retains selected aggregate/window phases and input ownership.
func ShiftLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), operationArg(interval))
}

// ShiftInstant adds a typed interval expression in an explicit timezone, preserving the instant's Go type.
func ShiftInstant[S any, V instantValue](input RowValue[S, V], interval RowValue[S, temporal.Interval], zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(ShiftInstantValue(rowInput(input).Value(), rowInput(interval).Value(), zone))
}

// ShiftInstantValue retains selected aggregate/window phases and input ownership.
func ShiftInstantValue[S any, V instantValue](input Selected[S, V], interval Selected[S, temporal.Interval], zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), operationArg(interval), zoneArgument(zone))
}

// ShiftInstantNullable adds a typed interval expression in an explicit timezone, preserving the instant's Go type. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval RowValue[S, value.Nullable[temporal.Interval]], zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(ShiftInstantNullableValue(rowInput(input).Value(), rowInput(interval).Value(), zone))
}

// ShiftInstantNullableValue retains selected aggregate/window phases and input ownership.
func ShiftInstantNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval Selected[S, value.Nullable[temporal.Interval]], zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), operationArg(interval), zoneArgument(zone))
}
