package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AddIntervals adds interval components without conflating calendar and elapsed units.
//
// Example: expr.AddIntervals(left, right)
func AddIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(AddIntervalsValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// AddIntervalsValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.AddIntervalsValue(left, right)
func AddIntervalsValue[S any](left Selected[S, temporal.Interval], right Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.AddIntervalsOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// AddIntervalsNullable adds interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.AddIntervalsNullable(left, right)
func AddIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(AddIntervalsNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// AddIntervalsNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.AddIntervalsNullableValue(left, right)
func AddIntervalsNullableValue[S any](left Selected[S, value.Nullable[temporal.Interval]], right Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.AddIntervalsOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// SubtractIntervals subtracts interval components without conflating calendar and elapsed units.
//
// Example: expr.SubtractIntervals(left, right)
func SubtractIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(SubtractIntervalsValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// SubtractIntervalsValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.SubtractIntervalsValue(left, right)
func SubtractIntervalsValue[S any](left Selected[S, temporal.Interval], right Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.SubtractIntervalsOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// SubtractIntervalsNullable subtracts interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.SubtractIntervalsNullable(left, right)
func SubtractIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(SubtractIntervalsNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// SubtractIntervalsNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.SubtractIntervalsNullableValue(left, right)
func SubtractIntervalsNullableValue[S any](left Selected[S, value.Nullable[temporal.Interval]], right Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.SubtractIntervalsOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// NegateInterval negates all interval components; database overflow remains an execution error.
//
// Example: expr.NegateInterval(input)
func NegateInterval[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(NegateIntervalValue(AsSelected(rowInput(input))))
}

// NegateIntervalValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.NegateIntervalValue(input)
func NegateIntervalValue[S any](input Selected[S, temporal.Interval]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.NegateIntervalOperation, columnCodec(codec.Interval()), operationArg(input))
}

// NegateIntervalNullable negates all interval components; database overflow remains an execution error. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.NegateIntervalNullable(input)
func NegateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(NegateIntervalNullableValue(AsSelected(rowInput(input))))
}

// NegateIntervalNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.NegateIntervalNullableValue(input)
func NegateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.NegateIntervalOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(input))
}

// LocalDifference subtracts local date-times using PostgreSQL's normalized interval result.
//
// Example: expr.LocalDifference(left, right)
func LocalDifference[S any](left RowValue[S, temporal.LocalDateTime], right RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(LocalDifferenceValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// LocalDifferenceValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.LocalDifferenceValue(left, right)
func LocalDifferenceValue[S any](left Selected[S, temporal.LocalDateTime], right Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.LocalDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// LocalDifferenceNullable subtracts local date-times using PostgreSQL's normalized interval result. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.LocalDifferenceNullable(left, right)
func LocalDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.LocalDateTime]], right RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(LocalDifferenceNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// LocalDifferenceNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.LocalDifferenceNullableValue(left, right)
func LocalDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.LocalDateTime]], right Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.LocalDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// InstantDifference subtracts instants using PostgreSQL's 24-hour-day interval normalization.
//
// Example: expr.InstantDifference(left, right)
func InstantDifference[S any, L, R instantValue](left RowValue[S, L], right RowValue[S, R]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(InstantDifferenceValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// InstantDifferenceValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.InstantDifferenceValue(left, right)
func InstantDifferenceValue[S any, L, R instantValue](left Selected[S, L], right Selected[S, R]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.InstantDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// InstantDifferenceNullable subtracts instants using PostgreSQL's 24-hour-day interval normalization. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.InstantDifferenceNullable(left, right)
func InstantDifferenceNullable[S any, L, R instantValue](left RowValue[S, value.Nullable[L]], right RowValue[S, value.Nullable[R]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(InstantDifferenceNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// InstantDifferenceNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.InstantDifferenceNullableValue(left, right)
func InstantDifferenceNullableValue[S any, L, R instantValue](left Selected[S, value.Nullable[L]], right Selected[S, value.Nullable[R]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.InstantDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// ClockDifference subtracts clock components without wrapping the signed result at midnight.
//
// Example: expr.ClockDifference(left, right)
func ClockDifference[S any](left RowValue[S, temporal.Time], right RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(ClockDifferenceValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// ClockDifferenceValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ClockDifferenceValue(left, right)
func ClockDifferenceValue[S any](left Selected[S, temporal.Time], right Selected[S, temporal.Time]) Selected[S, temporal.Interval] {
	return operationValue[S](querycore.ClockDifferenceOperation, columnCodec(codec.Interval()), operationArg(left), operationArg(right))
}

// ClockDifferenceNullable subtracts clock components without wrapping the signed result at midnight. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.ClockDifferenceNullable(left, right)
func ClockDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Time]], right RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(ClockDifferenceNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// ClockDifferenceNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ClockDifferenceNullableValue(left, right)
func ClockDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.Time]], right Selected[S, value.Nullable[temporal.Time]]) Selected[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](querycore.ClockDifferenceOperation, columnCodec(codec.Nullable(codec.Interval())), operationArg(left), operationArg(right))
}

// IntervalMonths returns the complete signed month component, including whole years.
//
// Example: expr.IntervalMonths(input)
func IntervalMonths[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalMonthsValue(AsSelected(rowInput(input))))
}

// IntervalMonthsValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalMonthsValue(input)
func IntervalMonthsValue[S any](input Selected[S, temporal.Interval]) Selected[S, int32] {
	return operationValue[S](querycore.IntervalMonthsOperation, columnCodec(codec.Signed[int32]()), operationArg(input))
}

// IntervalMonthsNullable returns the complete signed month component, including whole years. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.IntervalMonthsNullable(input)
func IntervalMonthsNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalMonthsNullableValue(AsSelected(rowInput(input))))
}

// IntervalMonthsNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalMonthsNullableValue(input)
func IntervalMonthsNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[int32]] {
	return operationValue[S](querycore.IntervalMonthsOperation, columnCodec(codec.Nullable(codec.Signed[int32]())), operationArg(input))
}

// IntervalDays returns the signed calendar-day component.
//
// Example: expr.IntervalDays(input)
func IntervalDays[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalDaysValue(AsSelected(rowInput(input))))
}

// IntervalDaysValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalDaysValue(input)
func IntervalDaysValue[S any](input Selected[S, temporal.Interval]) Selected[S, int32] {
	return operationValue[S](querycore.IntervalDaysOperation, columnCodec(codec.Signed[int32]()), operationArg(input))
}

// IntervalDaysNullable returns the signed calendar-day component. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.IntervalDaysNullable(input)
func IntervalDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalDaysNullableValue(AsSelected(rowInput(input))))
}

// IntervalDaysNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalDaysNullableValue(input)
func IntervalDaysNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[int32]] {
	return operationValue[S](querycore.IntervalDaysOperation, columnCodec(codec.Nullable(codec.Signed[int32]())), operationArg(input))
}

// IntervalElapsed returns the elapsed component as a Go duration, excluding months and days.
//
// Example: expr.IntervalElapsed(input)
func IntervalElapsed[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, time.Duration] {
	return orderedRow(IntervalElapsedValue(AsSelected(rowInput(input))))
}

// IntervalElapsedValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalElapsedValue(input)
func IntervalElapsedValue[S any](input Selected[S, temporal.Interval]) Selected[S, time.Duration] {
	return operationValue[S](querycore.IntervalElapsedOperation, columnCodec(codec.Signed[time.Duration]()), operationArg(input))
}

// IntervalElapsedNullable returns the elapsed component as a Go duration, excluding months and days. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.IntervalElapsedNullable(input)
func IntervalElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, time.Duration] {
	return nullableOrderedRow(IntervalElapsedNullableValue(AsSelected(rowInput(input))))
}

// IntervalElapsedNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.IntervalElapsedNullableValue(input)
func IntervalElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[time.Duration]] {
	return operationValue[S](querycore.IntervalElapsedOperation, columnCodec(codec.Nullable(codec.Signed[time.Duration]())), operationArg(input))
}

// ShiftDate adds a typed interval expression to a date, returning local date-time components.
//
// Example: expr.ShiftDate(input, interval)
func ShiftDate[S any](input RowValue[S, temporal.Date], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftDateValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval))))
}

// ShiftDateValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftDateValue(input, interval)
func ShiftDateValue[S any](input Selected[S, temporal.Date], interval Selected[S, temporal.Interval]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftDateNullable adds a typed interval expression to a date, returning local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.ShiftDateNullable(input, interval)
func ShiftDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftDateNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval))))
}

// ShiftDateNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftDateNullableValue(input, interval)
func ShiftDateNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), operationArg(interval))
}

// ShiftLocal adds a typed interval expression to local date-time components.
//
// Example: expr.ShiftLocal(input, interval)
func ShiftLocal[S any](input RowValue[S, temporal.LocalDateTime], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftLocalValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval))))
}

// ShiftLocalValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftLocalValue(input, interval)
func ShiftLocalValue[S any](input Selected[S, temporal.LocalDateTime], interval Selected[S, temporal.Interval]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftLocalNullable adds a typed interval expression to local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.ShiftLocalNullable(input, interval)
func ShiftLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftLocalNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval))))
}

// ShiftLocalNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftLocalNullableValue(input, interval)
func ShiftLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval Selected[S, value.Nullable[temporal.Interval]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), operationArg(interval))
}

// ShiftInstant adds a typed interval expression in an explicit timezone, preserving the instant's Go type.
//
// Example: expr.ShiftInstant(input, interval, zone)
func ShiftInstant[S any, V instantValue](input RowValue[S, V], interval RowValue[S, temporal.Interval], zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(ShiftInstantValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval)), zone))
}

// ShiftInstantValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftInstantValue(input, interval, zone)
func ShiftInstantValue[S any, V instantValue](input Selected[S, V], interval Selected[S, temporal.Interval], zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), operationArg(interval), zoneArgument(zone))
}

// ShiftInstantNullable adds a typed interval expression in an explicit timezone, preserving the instant's Go type. SQL NULL propagates; widen non-null inputs explicitly.
//
// Example: expr.ShiftInstantNullable(input, interval, zone)
func ShiftInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval RowValue[S, value.Nullable[temporal.Interval]], zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(ShiftInstantNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(interval)), zone))
}

// ShiftInstantNullableValue retains selected aggregate/window phases and input ownership.
//
// Example: expr.ShiftInstantNullableValue(input, interval, zone)
func ShiftInstantNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval Selected[S, value.Nullable[temporal.Interval]], zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), operationArg(interval), zoneArgument(zone))
}
