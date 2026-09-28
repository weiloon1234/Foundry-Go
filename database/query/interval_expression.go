package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

// AddIntervals adds interval components without conflating calendar and elapsed units.
func AddIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(AddIntervalsValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddIntervalsValue retains selected aggregate/window phases and input ownership.
func AddIntervalsValue[S any](left Expression[S, temporal.Interval], right Expression[S, temporal.Interval]) Expression[S, temporal.Interval] {
	return operationValue[S](addIntervalsOperation, codec.Interval(), operationArg(left), operationArg(right))
}

// AddIntervalsNullable adds interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
func AddIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(AddIntervalsNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddIntervalsNullableValue retains selected aggregate/window phases and input ownership.
func AddIntervalsNullableValue[S any](left Expression[S, value.Nullable[temporal.Interval]], right Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](addIntervalsOperation, codec.Nullable(codec.Interval()), operationArg(left), operationArg(right))
}

// SubtractIntervals subtracts interval components without conflating calendar and elapsed units.
func SubtractIntervals[S any](left RowValue[S, temporal.Interval], right RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(SubtractIntervalsValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractIntervalsValue retains selected aggregate/window phases and input ownership.
func SubtractIntervalsValue[S any](left Expression[S, temporal.Interval], right Expression[S, temporal.Interval]) Expression[S, temporal.Interval] {
	return operationValue[S](subtractIntervalsOperation, codec.Interval(), operationArg(left), operationArg(right))
}

// SubtractIntervalsNullable subtracts interval components without conflating calendar and elapsed units. SQL NULL propagates; widen non-null inputs explicitly.
func SubtractIntervalsNullable[S any](left RowValue[S, value.Nullable[temporal.Interval]], right RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(SubtractIntervalsNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractIntervalsNullableValue retains selected aggregate/window phases and input ownership.
func SubtractIntervalsNullableValue[S any](left Expression[S, value.Nullable[temporal.Interval]], right Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](subtractIntervalsOperation, codec.Nullable(codec.Interval()), operationArg(left), operationArg(right))
}

// NegateInterval negates all interval components; database overflow remains an execution error.
func NegateInterval[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(NegateIntervalValue(rowInput(input).Value()))
}

// NegateIntervalValue retains selected aggregate/window phases and input ownership.
func NegateIntervalValue[S any](input Expression[S, temporal.Interval]) Expression[S, temporal.Interval] {
	return operationValue[S](negateIntervalOperation, codec.Interval(), operationArg(input))
}

// NegateIntervalNullable negates all interval components; database overflow remains an execution error. SQL NULL propagates; widen non-null inputs explicitly.
func NegateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(NegateIntervalNullableValue(rowInput(input).Value()))
}

// NegateIntervalNullableValue retains selected aggregate/window phases and input ownership.
func NegateIntervalNullableValue[S any](input Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](negateIntervalOperation, codec.Nullable(codec.Interval()), operationArg(input))
}

// LocalDifference subtracts local date-times using PostgreSQL's normalized interval result.
func LocalDifference[S any](left RowValue[S, temporal.LocalDateTime], right RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(LocalDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// LocalDifferenceValue retains selected aggregate/window phases and input ownership.
func LocalDifferenceValue[S any](left Expression[S, temporal.LocalDateTime], right Expression[S, temporal.LocalDateTime]) Expression[S, temporal.Interval] {
	return operationValue[S](localDifferenceOperation, codec.Interval(), operationArg(left), operationArg(right))
}

// LocalDifferenceNullable subtracts local date-times using PostgreSQL's normalized interval result. SQL NULL propagates; widen non-null inputs explicitly.
func LocalDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.LocalDateTime]], right RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(LocalDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// LocalDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func LocalDifferenceNullableValue[S any](left Expression[S, value.Nullable[temporal.LocalDateTime]], right Expression[S, value.Nullable[temporal.LocalDateTime]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](localDifferenceOperation, codec.Nullable(codec.Interval()), operationArg(left), operationArg(right))
}

// InstantDifference subtracts instants using PostgreSQL's 24-hour-day interval normalization.
func InstantDifference[S any, L, R instantValue](left RowValue[S, L], right RowValue[S, R]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(InstantDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// InstantDifferenceValue retains selected aggregate/window phases and input ownership.
func InstantDifferenceValue[S any, L, R instantValue](left Expression[S, L], right Expression[S, R]) Expression[S, temporal.Interval] {
	return operationValue[S](instantDifferenceOperation, codec.Interval(), operationArg(left), operationArg(right))
}

// InstantDifferenceNullable subtracts instants using PostgreSQL's 24-hour-day interval normalization. SQL NULL propagates; widen non-null inputs explicitly.
func InstantDifferenceNullable[S any, L, R instantValue](left RowValue[S, value.Nullable[L]], right RowValue[S, value.Nullable[R]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(InstantDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// InstantDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func InstantDifferenceNullableValue[S any, L, R instantValue](left Expression[S, value.Nullable[L]], right Expression[S, value.Nullable[R]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](instantDifferenceOperation, codec.Nullable(codec.Interval()), operationArg(left), operationArg(right))
}

// ClockDifference subtracts clock components without wrapping the signed result at midnight.
func ClockDifference[S any](left RowValue[S, temporal.Time], right RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.Interval] {
	return orderedRow(ClockDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// ClockDifferenceValue retains selected aggregate/window phases and input ownership.
func ClockDifferenceValue[S any](left Expression[S, temporal.Time], right Expression[S, temporal.Time]) Expression[S, temporal.Interval] {
	return operationValue[S](clockDifferenceOperation, codec.Interval(), operationArg(left), operationArg(right))
}

// ClockDifferenceNullable subtracts clock components without wrapping the signed result at midnight. SQL NULL propagates; widen non-null inputs explicitly.
func ClockDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Time]], right RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.Interval] {
	return nullableOrderedRow(ClockDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// ClockDifferenceNullableValue retains selected aggregate/window phases and input ownership.
func ClockDifferenceNullableValue[S any](left Expression[S, value.Nullable[temporal.Time]], right Expression[S, value.Nullable[temporal.Time]]) Expression[S, value.Nullable[temporal.Interval]] {
	return operationValue[S](clockDifferenceOperation, codec.Nullable(codec.Interval()), operationArg(left), operationArg(right))
}

// IntervalMonths returns the complete signed month component, including whole years.
func IntervalMonths[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalMonthsValue(rowInput(input).Value()))
}

// IntervalMonthsValue retains selected aggregate/window phases and input ownership.
func IntervalMonthsValue[S any](input Expression[S, temporal.Interval]) Expression[S, int32] {
	return operationValue[S](intervalMonthsOperation, codec.Signed[int32](), operationArg(input))
}

// IntervalMonthsNullable returns the complete signed month component, including whole years. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalMonthsNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalMonthsNullableValue(rowInput(input).Value()))
}

// IntervalMonthsNullableValue retains selected aggregate/window phases and input ownership.
func IntervalMonthsNullableValue[S any](input Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[int32]] {
	return operationValue[S](intervalMonthsOperation, codec.Nullable(codec.Signed[int32]()), operationArg(input))
}

// IntervalDays returns the signed calendar-day component.
func IntervalDays[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, int32] {
	return orderedRow(IntervalDaysValue(rowInput(input).Value()))
}

// IntervalDaysValue retains selected aggregate/window phases and input ownership.
func IntervalDaysValue[S any](input Expression[S, temporal.Interval]) Expression[S, int32] {
	return operationValue[S](intervalDaysOperation, codec.Signed[int32](), operationArg(input))
}

// IntervalDaysNullable returns the signed calendar-day component. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, int32] {
	return nullableOrderedRow(IntervalDaysNullableValue(rowInput(input).Value()))
}

// IntervalDaysNullableValue retains selected aggregate/window phases and input ownership.
func IntervalDaysNullableValue[S any](input Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[int32]] {
	return operationValue[S](intervalDaysOperation, codec.Nullable(codec.Signed[int32]()), operationArg(input))
}

// IntervalElapsed returns the elapsed component as a Go duration, excluding months and days.
func IntervalElapsed[S any](input RowValue[S, temporal.Interval]) OrderedRowExpression[S, time.Duration] {
	return orderedRow(IntervalElapsedValue(rowInput(input).Value()))
}

// IntervalElapsedValue retains selected aggregate/window phases and input ownership.
func IntervalElapsedValue[S any](input Expression[S, temporal.Interval]) Expression[S, time.Duration] {
	return operationValue[S](intervalElapsedOperation, codec.Signed[time.Duration](), operationArg(input))
}

// IntervalElapsedNullable returns the elapsed component as a Go duration, excluding months and days. SQL NULL propagates; widen non-null inputs explicitly.
func IntervalElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, time.Duration] {
	return nullableOrderedRow(IntervalElapsedNullableValue(rowInput(input).Value()))
}

// IntervalElapsedNullableValue retains selected aggregate/window phases and input ownership.
func IntervalElapsedNullableValue[S any](input Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[time.Duration]] {
	return operationValue[S](intervalElapsedOperation, codec.Nullable(codec.Signed[time.Duration]()), operationArg(input))
}

// ShiftDate adds a typed interval expression to a date, returning local date-time components.
func ShiftDate[S any](input RowValue[S, temporal.Date], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftDateValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftDateValue retains selected aggregate/window phases and input ownership.
func ShiftDateValue[S any](input Expression[S, temporal.Date], interval Expression[S, temporal.Interval]) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](addDateIntervalOperation, codec.LocalDateTime(), operationArg(input), operationArg(interval))
}

// ShiftDateNullable adds a typed interval expression to a date, returning local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftDateNullableValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftDateNullableValue retains selected aggregate/window phases and input ownership.
func ShiftDateNullableValue[S any](input Expression[S, value.Nullable[temporal.Date]], interval Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](addDateIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftLocal adds a typed interval expression to local date-time components.
func ShiftLocal[S any](input RowValue[S, temporal.LocalDateTime], interval RowValue[S, temporal.Interval]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(ShiftLocalValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftLocalValue retains selected aggregate/window phases and input ownership.
func ShiftLocalValue[S any](input Expression[S, temporal.LocalDateTime], interval Expression[S, temporal.Interval]) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](addLocalIntervalOperation, codec.LocalDateTime(), operationArg(input), operationArg(interval))
}

// ShiftLocalNullable adds a typed interval expression to local date-time components. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval RowValue[S, value.Nullable[temporal.Interval]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(ShiftLocalNullableValue(rowInput(input).Value(), rowInput(interval).Value()))
}

// ShiftLocalNullableValue retains selected aggregate/window phases and input ownership.
func ShiftLocalNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]], interval Expression[S, value.Nullable[temporal.Interval]]) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](addLocalIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), operationArg(interval))
}

// ShiftInstant adds a typed interval expression in an explicit timezone, preserving the instant's Go type.
func ShiftInstant[S any, V instantValue](input RowValue[S, V], interval RowValue[S, temporal.Interval], zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(ShiftInstantValue(rowInput(input).Value(), rowInput(interval).Value(), zone))
}

// ShiftInstantValue retains selected aggregate/window phases and input ownership.
func ShiftInstantValue[S any, V instantValue](input Expression[S, V], interval Expression[S, temporal.Interval], zone TimeZone) Expression[S, V] {
	return operationValue[S](addInstantIntervalOperation, input.codec, operationArg(input), operationArg(interval), zoneArgument[S](zone))
}

// ShiftInstantNullable adds a typed interval expression in an explicit timezone, preserving the instant's Go type. SQL NULL propagates; widen non-null inputs explicitly.
func ShiftInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval RowValue[S, value.Nullable[temporal.Interval]], zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(ShiftInstantNullableValue(rowInput(input).Value(), rowInput(interval).Value(), zone))
}

// ShiftInstantNullableValue retains selected aggregate/window phases and input ownership.
func ShiftInstantNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]], interval Expression[S, value.Nullable[temporal.Interval]], zone TimeZone) Expression[S, value.Nullable[V]] {
	return operationValue[S](addInstantIntervalOperation, input.codec, operationArg(input), operationArg(interval), zoneArgument[S](zone))
}
