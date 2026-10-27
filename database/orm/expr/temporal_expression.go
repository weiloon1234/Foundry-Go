package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TruncateDate truncates a calendar date without introducing a timezone.
func TruncateDate[S any](input RowValue[S, temporal.Date], unit DateUnit) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(TruncateDateValue(rowInput(input).Value(), unit))
}

// TruncateDateValue retains the selected aggregate/window phase.
func TruncateDateValue[S any](input Selected[S, temporal.Date], unit DateUnit) Selected[S, temporal.Date] {
	return operationValue[S](querycore.TruncateDateOperation, columnCodec(codec.Date()), unitArgument(temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateDateNullable truncates a calendar date without introducing a timezone. SQL NULL propagates; widen non-null operands explicitly.
func TruncateDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], unit DateUnit) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(TruncateDateNullableValue(rowInput(input).Value(), unit))
}

// TruncateDateNullableValue retains the selected aggregate/window phase.
func TruncateDateNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], unit DateUnit) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.TruncateDateOperation, columnCodec(codec.Nullable(codec.Date())), unitArgument(temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateLocal truncates local wall components without interpreting them as an instant.
func TruncateLocal[S any](input RowValue[S, temporal.LocalDateTime], unit TimestampUnit) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(TruncateLocalValue(rowInput(input).Value(), unit))
}

// TruncateLocalValue retains the selected aggregate/window phase.
func TruncateLocalValue[S any](input Selected[S, temporal.LocalDateTime], unit TimestampUnit) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.TruncateLocalOperation, columnCodec(codec.LocalDateTime()), unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateLocalNullable truncates local wall components without interpreting them as an instant. SQL NULL propagates; widen non-null operands explicitly.
func TruncateLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(TruncateLocalNullableValue(rowInput(input).Value(), unit))
}

// TruncateLocalNullableValue retains the selected aggregate/window phase.
func TruncateLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.TruncateLocalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateInstant truncates an instant in the explicit zone while preserving its Go type.
func TruncateInstant[S any, V instantValue](input RowValue[S, V], unit TimestampUnit, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(TruncateInstantValue(rowInput(input).Value(), unit, zone))
}

// TruncateInstantValue retains the selected aggregate/window phase.
func TruncateInstantValue[S any, V instantValue](input Selected[S, V], unit TimestampUnit, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.TruncateInstantOperation, input.data.column, unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument(zone))
}

// TruncateInstantNullable truncates an instant in the explicit zone while preserving its Go type. SQL NULL propagates; widen non-null operands explicitly.
func TruncateInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(TruncateInstantNullableValue(rowInput(input).Value(), unit, zone))
}

// TruncateInstantNullableValue retains the selected aggregate/window phase.
func TruncateInstantNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.TruncateInstantOperation, input.data.column, unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument(zone))
}

// FromUnixMillis converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding.
func FromUnixMillis[S any, V integerNumber](input RowValue[S, V]) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(FromUnixMillisValue(rowInput(input).Value()))
}

// FromUnixMillisValue retains the selected aggregate/window phase.
func FromUnixMillisValue[S any, V integerNumber](input Selected[S, V]) Selected[S, temporal.DateTime] {
	return operationValue[S](querycore.FromUnixMillisOperation, columnCodec(codec.DateTime()), operationArg(input))
}

// FromUnixMillisNullable converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding. SQL NULL propagates; widen non-null operands explicitly.
func FromUnixMillisNullable[S any, V integerNumber](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(FromUnixMillisNullableValue(rowInput(input).Value()))
}

// FromUnixMillisNullableValue retains the selected aggregate/window phase.
func FromUnixMillisNullableValue[S any, V integerNumber](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](querycore.FromUnixMillisOperation, columnCodec(codec.Nullable(codec.DateTime())), operationArg(input))
}

// LocalAt converts an instant to wall components in the explicitly selected zone.
func LocalAt[S any, V instantValue](input RowValue[S, V], zone TimeZone) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(LocalAtValue(rowInput(input).Value(), zone))
}

// LocalAtValue retains the selected aggregate/window phase.
func LocalAtValue[S any, V instantValue](input Selected[S, V], zone TimeZone) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.LocalAtOperation, columnCodec(codec.LocalDateTime()), zoneArgument(zone), operationArg(input))
}

// LocalAtNullable converts an instant to wall components in the explicitly selected zone. SQL NULL propagates; widen non-null operands explicitly.
func LocalAtNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], zone TimeZone) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(LocalAtNullableValue(rowInput(input).Value(), zone))
}

// LocalAtNullableValue retains the selected aggregate/window phase.
func LocalAtNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], zone TimeZone) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.LocalAtOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), zoneArgument(zone), operationArg(input))
}

// ResolveLocal uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In.
func ResolveLocal[S any](input RowValue[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(ResolveLocalValue(rowInput(input).Value(), zone, policy))
}

// ResolveLocalValue retains the selected aggregate/window phase.
func ResolveLocalValue[S any](input Selected[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) Selected[S, temporal.DateTime] {
	return operationValue[S](querycore.ResolveLocalOperation, columnCodec(codec.DateTime()), resolutionArgument(zone, policy), operationArg(input))
}

// ResolveLocalNullable uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In. SQL NULL propagates; widen non-null operands explicitly.
func ResolveLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(ResolveLocalNullableValue(rowInput(input).Value(), zone, policy))
}

// ResolveLocalNullableValue retains the selected aggregate/window phase.
func ResolveLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) Selected[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](querycore.ResolveLocalOperation, columnCodec(codec.Nullable(codec.DateTime())), resolutionArgument(zone, policy), operationArg(input))
}

// DateOf selects the calendar date from local wall components.
func DateOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(DateOfValue(rowInput(input).Value()))
}

// DateOfValue retains the selected aggregate/window phase.
func DateOfValue[S any](input Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Date] {
	return operationValue[S](querycore.DateOfOperation, columnCodec(codec.Date()), operationArg(input))
}

// DateOfNullable selects the calendar date from local wall components. SQL NULL propagates; widen non-null operands explicitly.
func DateOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(DateOfNullableValue(rowInput(input).Value()))
}

// DateOfNullableValue retains the selected aggregate/window phase.
func DateOfNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.DateOfOperation, columnCodec(codec.Nullable(codec.Date())), operationArg(input))
}

// TimeOf selects the time of day from local wall components.
func TimeOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(TimeOfValue(rowInput(input).Value()))
}

// TimeOfValue retains the selected aggregate/window phase.
func TimeOfValue[S any](input Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Time] {
	return operationValue[S](querycore.TimeOfOperation, columnCodec(codec.WallTime()), operationArg(input))
}

// TimeOfNullable selects the time of day from local wall components. SQL NULL propagates; widen non-null operands explicitly.
func TimeOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(TimeOfNullableValue(rowInput(input).Value()))
}

// TimeOfNullableValue retains the selected aggregate/window phase.
func TimeOfNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.TimeOfOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input))
}

// CombineDateTime combines a calendar date and time of day without choosing a timezone.
func CombineDateTime[S any](date RowValue[S, temporal.Date], clock RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(CombineDateTimeValue(rowInput(date).Value(), rowInput(clock).Value()))
}

// CombineDateTimeValue retains the selected aggregate/window phase.
func CombineDateTimeValue[S any](date Selected[S, temporal.Date], clock Selected[S, temporal.Time]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.CombineDateTimeOperation, columnCodec(codec.LocalDateTime()), operationArg(date), operationArg(clock))
}

// CombineDateTimeNullable combines a calendar date and time of day without choosing a timezone. SQL NULL propagates; widen non-null operands explicitly.
func CombineDateTimeNullable[S any](date RowValue[S, value.Nullable[temporal.Date]], clock RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(CombineDateTimeNullableValue(rowInput(date).Value(), rowInput(clock).Value()))
}

// CombineDateTimeNullableValue retains the selected aggregate/window phase.
func CombineDateTimeNullableValue[S any](date Selected[S, value.Nullable[temporal.Date]], clock Selected[S, value.Nullable[temporal.Time]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.CombineDateTimeOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(date), operationArg(clock))
}

// AddDateDays adds signed calendar days and retains the date-only result.
func AddDateDays[S any](input RowValue[S, temporal.Date], days int32) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(AddDateDaysValue(rowInput(input).Value(), days))
}

// AddDateDaysValue retains the selected aggregate/window phase.
func AddDateDaysValue[S any](input Selected[S, temporal.Date], days int32) Selected[S, temporal.Date] {
	return operationValue[S](querycore.AddDateDaysOperation, columnCodec(codec.Date()), operationArg(input), integerArgument(days))
}

// AddDateDaysNullable adds signed calendar days and retains the date-only result. SQL NULL propagates; widen non-null operands explicitly.
func AddDateDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], days int32) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(AddDateDaysNullableValue(rowInput(input).Value(), days))
}

// AddDateDaysNullableValue retains the selected aggregate/window phase.
func AddDateDaysNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], days int32) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.AddDateDaysOperation, columnCodec(codec.Nullable(codec.Date())), operationArg(input), integerArgument(days))
}

// DateDifference returns the signed number of calendar days from right to left.
func DateDifference[S any](left RowValue[S, temporal.Date], right RowValue[S, temporal.Date]) OrderedRowExpression[S, int64] {
	return orderedRow(DateDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DateDifferenceValue retains the selected aggregate/window phase.
func DateDifferenceValue[S any](left Selected[S, temporal.Date], right Selected[S, temporal.Date]) Selected[S, int64] {
	return operationValue[S](querycore.DateDifferenceOperation, columnCodec(codec.Signed[int64]()), operationArg(left), operationArg(right))
}

// DateDifferenceNullable returns the signed number of calendar days from right to left. SQL NULL propagates; widen non-null operands explicitly.
func DateDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Date]], right RowValue[S, value.Nullable[temporal.Date]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DateDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DateDifferenceNullableValue retains the selected aggregate/window phase.
func DateDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.Date]], right Selected[S, value.Nullable[temporal.Date]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.DateDifferenceOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(left), operationArg(right))
}

// AddDateInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func AddDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddDateIntervalValue(rowInput(input).Value(), interval))
}

// AddDateIntervalValue retains the selected aggregate/window phase.
func AddDateIntervalValue[S any](input Selected[S, temporal.Date], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// AddDateIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddDateIntervalNullableValue(rowInput(input).Value(), interval))
}

// AddDateIntervalNullableValue retains the selected aggregate/window phase.
func AddDateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// SubtractDateInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func SubtractDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractDateIntervalValue(rowInput(input).Value(), interval))
}

// SubtractDateIntervalValue retains the selected aggregate/window phase.
func SubtractDateIntervalValue[S any](input Selected[S, temporal.Date], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.SubtractDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// SubtractDateIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractDateIntervalNullableValue(rowInput(input).Value(), interval))
}

// SubtractDateIntervalNullableValue retains the selected aggregate/window phase.
func SubtractDateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.SubtractDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// AddLocalInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func AddLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddLocalIntervalValue(rowInput(input).Value(), interval))
}

// AddLocalIntervalValue retains the selected aggregate/window phase.
func AddLocalIntervalValue[S any](input Selected[S, temporal.LocalDateTime], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// AddLocalIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddLocalIntervalNullableValue(rowInput(input).Value(), interval))
}

// AddLocalIntervalNullableValue retains the selected aggregate/window phase.
func AddLocalIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// SubtractLocalInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func SubtractLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractLocalIntervalValue(rowInput(input).Value(), interval))
}

// SubtractLocalIntervalValue retains the selected aggregate/window phase.
func SubtractLocalIntervalValue[S any](input Selected[S, temporal.LocalDateTime], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.SubtractLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// SubtractLocalIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractLocalIntervalNullableValue(rowInput(input).Value(), interval))
}

// SubtractLocalIntervalNullableValue retains the selected aggregate/window phase.
func SubtractLocalIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.SubtractLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// AddInstantInterval adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
func AddInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(AddInstantIntervalValue(rowInput(input).Value(), interval, zone))
}

// AddInstantIntervalValue retains the selected aggregate/window phase.
func AddInstantIntervalValue[S any, V instantValue](input Selected[S, V], interval temporal.Interval, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// AddInstantIntervalNullable adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AddInstantIntervalNullableValue(rowInput(input).Value(), interval, zone))
}

// AddInstantIntervalNullableValue retains the selected aggregate/window phase.
func AddInstantIntervalNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// SubtractInstantInterval subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
func SubtractInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(SubtractInstantIntervalValue(rowInput(input).Value(), interval, zone))
}

// SubtractInstantIntervalValue retains the selected aggregate/window phase.
func SubtractInstantIntervalValue[S any, V instantValue](input Selected[S, V], interval temporal.Interval, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.SubtractInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// SubtractInstantIntervalNullable subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(SubtractInstantIntervalNullableValue(rowInput(input).Value(), interval, zone))
}

// SubtractInstantIntervalNullableValue retains the selected aggregate/window phase.
func SubtractInstantIntervalNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.SubtractInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// AddClockElapsed adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required.
func AddClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(AddClockElapsedValue(rowInput(input).Value(), duration))
}

// AddClockElapsedValue retains the selected aggregate/window phase.
func AddClockElapsedValue[S any](input Selected[S, temporal.Time], duration time.Duration) Selected[S, temporal.Time] {
	return operationValue[S](querycore.AddClockElapsedOperation, columnCodec(codec.WallTime()), operationArg(input), elapsedArgument(duration))
}

// AddClockElapsedNullable adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
func AddClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(AddClockElapsedNullableValue(rowInput(input).Value(), duration))
}

// AddClockElapsedNullableValue retains the selected aggregate/window phase.
func AddClockElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Time]], duration time.Duration) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.AddClockElapsedOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input), elapsedArgument(duration))
}

// SubtractClockElapsed subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required.
func SubtractClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(SubtractClockElapsedValue(rowInput(input).Value(), duration))
}

// SubtractClockElapsedValue retains the selected aggregate/window phase.
func SubtractClockElapsedValue[S any](input Selected[S, temporal.Time], duration time.Duration) Selected[S, temporal.Time] {
	return operationValue[S](querycore.SubtractClockElapsedOperation, columnCodec(codec.WallTime()), operationArg(input), elapsedArgument(duration))
}

// SubtractClockElapsedNullable subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
func SubtractClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(SubtractClockElapsedNullableValue(rowInput(input).Value(), duration))
}

// SubtractClockElapsedNullableValue retains the selected aggregate/window phase.
func SubtractClockElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Time]], duration time.Duration) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.SubtractClockElapsedOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input), elapsedArgument(duration))
}

// TransactionTime returns PostgreSQL's transaction start instant. It is stable
// throughout a transaction and does not use the application's test clock.
func TransactionTime[S, M any](scope Scope[S, M]) OrderedRowExpression[S, temporal.DateTime] {
	result := operationValue[S](querycore.TransactionTimeOperation, columnCodec(codec.DateTime()))
	result.data.core.Refs = []ormcore.Reference{scope.core.Reference, {Scope: reflect.TypeFor[S]()}}
	result.data.core.Err = scope.core.Err
	if scope.core.Owner == nil || scope.core.Table == "" {
		result.data.core.Err = safeerror.Join("operation failed", result.data.core.Err, ormcore.Invalid("database time requires a source scope"))
	}
	return orderedRow(result)
}
