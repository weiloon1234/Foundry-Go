package query

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TruncateDate truncates a calendar date without introducing a timezone.
func TruncateDate[S any](input RowValue[S, temporal.Date], unit DateUnit) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(TruncateDateValue(rowInput(input).Value(), unit))
}

// TruncateDateValue retains the selected aggregate/window phase.
func TruncateDateValue[S any](input Expression[S, temporal.Date], unit DateUnit) Expression[S, temporal.Date] {
	return operationValue[S](truncateDateOperation, codec.Date(), unitArgument[S](temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateDateNullable truncates a calendar date without introducing a timezone. SQL NULL propagates; widen non-null operands explicitly.
func TruncateDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], unit DateUnit) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(TruncateDateNullableValue(rowInput(input).Value(), unit))
}

// TruncateDateNullableValue retains the selected aggregate/window phase.
func TruncateDateNullableValue[S any](input Expression[S, value.Nullable[temporal.Date]], unit DateUnit) Expression[S, value.Nullable[temporal.Date]] {
	return operationValue[S](truncateDateOperation, codec.Nullable(codec.Date()), unitArgument[S](temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateLocal truncates local wall components without interpreting them as an instant.
func TruncateLocal[S any](input RowValue[S, temporal.LocalDateTime], unit TimestampUnit) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(TruncateLocalValue(rowInput(input).Value(), unit))
}

// TruncateLocalValue retains the selected aggregate/window phase.
func TruncateLocalValue[S any](input Expression[S, temporal.LocalDateTime], unit TimestampUnit) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](truncateLocalOperation, codec.LocalDateTime(), unitArgument[S](temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateLocalNullable truncates local wall components without interpreting them as an instant. SQL NULL propagates; widen non-null operands explicitly.
func TruncateLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(TruncateLocalNullableValue(rowInput(input).Value(), unit))
}

// TruncateLocalNullableValue retains the selected aggregate/window phase.
func TruncateLocalNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](truncateLocalOperation, codec.Nullable(codec.LocalDateTime()), unitArgument[S](temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateInstant truncates an instant in the explicit zone while preserving its Go type.
func TruncateInstant[S any, V instantValue](input RowValue[S, V], unit TimestampUnit, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(TruncateInstantValue(rowInput(input).Value(), unit, zone))
}

// TruncateInstantValue retains the selected aggregate/window phase.
func TruncateInstantValue[S any, V instantValue](input Expression[S, V], unit TimestampUnit, zone TimeZone) Expression[S, V] {
	return operationValue[S](truncateInstantOperation, input.codec, unitArgument[S](temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument[S](zone))
}

// TruncateInstantNullable truncates an instant in the explicit zone while preserving its Go type. SQL NULL propagates; widen non-null operands explicitly.
func TruncateInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(TruncateInstantNullableValue(rowInput(input).Value(), unit, zone))
}

// TruncateInstantNullableValue retains the selected aggregate/window phase.
func TruncateInstantNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) Expression[S, value.Nullable[V]] {
	return operationValue[S](truncateInstantOperation, input.codec, unitArgument[S](temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument[S](zone))
}

// FromUnixMillis converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding.
func FromUnixMillis[S any, V integerNumber](input RowValue[S, V]) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(FromUnixMillisValue(rowInput(input).Value()))
}

// FromUnixMillisValue retains the selected aggregate/window phase.
func FromUnixMillisValue[S any, V integerNumber](input Expression[S, V]) Expression[S, temporal.DateTime] {
	return operationValue[S](fromUnixMillisOperation, codec.DateTime(), operationArg(input))
}

// FromUnixMillisNullable converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding. SQL NULL propagates; widen non-null operands explicitly.
func FromUnixMillisNullable[S any, V integerNumber](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(FromUnixMillisNullableValue(rowInput(input).Value()))
}

// FromUnixMillisNullableValue retains the selected aggregate/window phase.
func FromUnixMillisNullableValue[S any, V integerNumber](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](fromUnixMillisOperation, codec.Nullable(codec.DateTime()), operationArg(input))
}

// LocalAt converts an instant to wall components in the explicitly selected zone.
func LocalAt[S any, V instantValue](input RowValue[S, V], zone TimeZone) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(LocalAtValue(rowInput(input).Value(), zone))
}

// LocalAtValue retains the selected aggregate/window phase.
func LocalAtValue[S any, V instantValue](input Expression[S, V], zone TimeZone) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](localAtOperation, codec.LocalDateTime(), zoneArgument[S](zone), operationArg(input))
}

// LocalAtNullable converts an instant to wall components in the explicitly selected zone. SQL NULL propagates; widen non-null operands explicitly.
func LocalAtNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], zone TimeZone) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(LocalAtNullableValue(rowInput(input).Value(), zone))
}

// LocalAtNullableValue retains the selected aggregate/window phase.
func LocalAtNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]], zone TimeZone) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](localAtOperation, codec.Nullable(codec.LocalDateTime()), zoneArgument[S](zone), operationArg(input))
}

// ResolveLocal uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In.
func ResolveLocal[S any](input RowValue[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(ResolveLocalValue(rowInput(input).Value(), zone, policy))
}

// ResolveLocalValue retains the selected aggregate/window phase.
func ResolveLocalValue[S any](input Expression[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) Expression[S, temporal.DateTime] {
	return operationValue[S](resolveLocalOperation, codec.DateTime(), resolutionArgument[S](zone, policy), operationArg(input))
}

// ResolveLocalNullable uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In. SQL NULL propagates; widen non-null operands explicitly.
func ResolveLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(ResolveLocalNullableValue(rowInput(input).Value(), zone, policy))
}

// ResolveLocalNullableValue retains the selected aggregate/window phase.
func ResolveLocalNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) Expression[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](resolveLocalOperation, codec.Nullable(codec.DateTime()), resolutionArgument[S](zone, policy), operationArg(input))
}

// DateOf selects the calendar date from local wall components.
func DateOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(DateOfValue(rowInput(input).Value()))
}

// DateOfValue retains the selected aggregate/window phase.
func DateOfValue[S any](input Expression[S, temporal.LocalDateTime]) Expression[S, temporal.Date] {
	return operationValue[S](dateOfOperation, codec.Date(), operationArg(input))
}

// DateOfNullable selects the calendar date from local wall components. SQL NULL propagates; widen non-null operands explicitly.
func DateOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(DateOfNullableValue(rowInput(input).Value()))
}

// DateOfNullableValue retains the selected aggregate/window phase.
func DateOfNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]]) Expression[S, value.Nullable[temporal.Date]] {
	return operationValue[S](dateOfOperation, codec.Nullable(codec.Date()), operationArg(input))
}

// TimeOf selects the time of day from local wall components.
func TimeOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(TimeOfValue(rowInput(input).Value()))
}

// TimeOfValue retains the selected aggregate/window phase.
func TimeOfValue[S any](input Expression[S, temporal.LocalDateTime]) Expression[S, temporal.Time] {
	return operationValue[S](timeOfOperation, codec.WallTime(), operationArg(input))
}

// TimeOfNullable selects the time of day from local wall components. SQL NULL propagates; widen non-null operands explicitly.
func TimeOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(TimeOfNullableValue(rowInput(input).Value()))
}

// TimeOfNullableValue retains the selected aggregate/window phase.
func TimeOfNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]]) Expression[S, value.Nullable[temporal.Time]] {
	return operationValue[S](timeOfOperation, codec.Nullable(codec.WallTime()), operationArg(input))
}

// CombineDateTime combines a calendar date and time of day without choosing a timezone.
func CombineDateTime[S any](date RowValue[S, temporal.Date], clock RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(CombineDateTimeValue(rowInput(date).Value(), rowInput(clock).Value()))
}

// CombineDateTimeValue retains the selected aggregate/window phase.
func CombineDateTimeValue[S any](date Expression[S, temporal.Date], clock Expression[S, temporal.Time]) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](combineDateTimeOperation, codec.LocalDateTime(), operationArg(date), operationArg(clock))
}

// CombineDateTimeNullable combines a calendar date and time of day without choosing a timezone. SQL NULL propagates; widen non-null operands explicitly.
func CombineDateTimeNullable[S any](date RowValue[S, value.Nullable[temporal.Date]], clock RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(CombineDateTimeNullableValue(rowInput(date).Value(), rowInput(clock).Value()))
}

// CombineDateTimeNullableValue retains the selected aggregate/window phase.
func CombineDateTimeNullableValue[S any](date Expression[S, value.Nullable[temporal.Date]], clock Expression[S, value.Nullable[temporal.Time]]) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](combineDateTimeOperation, codec.Nullable(codec.LocalDateTime()), operationArg(date), operationArg(clock))
}

// AddDateDays adds signed calendar days and retains the date-only result.
func AddDateDays[S any](input RowValue[S, temporal.Date], days int32) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(AddDateDaysValue(rowInput(input).Value(), days))
}

// AddDateDaysValue retains the selected aggregate/window phase.
func AddDateDaysValue[S any](input Expression[S, temporal.Date], days int32) Expression[S, temporal.Date] {
	return operationValue[S](addDateDaysOperation, codec.Date(), operationArg(input), operationArg(parameterExpression[S](days, codec.Signed[int32]()).Value()))
}

// AddDateDaysNullable adds signed calendar days and retains the date-only result. SQL NULL propagates; widen non-null operands explicitly.
func AddDateDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], days int32) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(AddDateDaysNullableValue(rowInput(input).Value(), days))
}

// AddDateDaysNullableValue retains the selected aggregate/window phase.
func AddDateDaysNullableValue[S any](input Expression[S, value.Nullable[temporal.Date]], days int32) Expression[S, value.Nullable[temporal.Date]] {
	return operationValue[S](addDateDaysOperation, codec.Nullable(codec.Date()), operationArg(input), operationArg(parameterExpression[S](days, codec.Signed[int32]()).Value()))
}

// DateDifference returns the signed number of calendar days from right to left.
func DateDifference[S any](left RowValue[S, temporal.Date], right RowValue[S, temporal.Date]) OrderedRowExpression[S, int64] {
	return orderedRow(DateDifferenceValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DateDifferenceValue retains the selected aggregate/window phase.
func DateDifferenceValue[S any](left Expression[S, temporal.Date], right Expression[S, temporal.Date]) Expression[S, int64] {
	return operationValue[S](dateDifferenceOperation, codec.Signed[int64](), operationArg(left), operationArg(right))
}

// DateDifferenceNullable returns the signed number of calendar days from right to left. SQL NULL propagates; widen non-null operands explicitly.
func DateDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Date]], right RowValue[S, value.Nullable[temporal.Date]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DateDifferenceNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DateDifferenceNullableValue retains the selected aggregate/window phase.
func DateDifferenceNullableValue[S any](left Expression[S, value.Nullable[temporal.Date]], right Expression[S, value.Nullable[temporal.Date]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](dateDifferenceOperation, codec.Nullable(codec.Signed[int64]()), operationArg(left), operationArg(right))
}

// AddDateInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func AddDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddDateIntervalValue(rowInput(input).Value(), interval))
}

// AddDateIntervalValue retains the selected aggregate/window phase.
func AddDateIntervalValue[S any](input Expression[S, temporal.Date], interval temporal.Interval) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](addDateIntervalOperation, codec.LocalDateTime(), operationArg(input), intervalArgument[S](interval))
}

// AddDateIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddDateIntervalNullableValue(rowInput(input).Value(), interval))
}

// AddDateIntervalNullableValue retains the selected aggregate/window phase.
func AddDateIntervalNullableValue[S any](input Expression[S, value.Nullable[temporal.Date]], interval temporal.Interval) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](addDateIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), intervalArgument[S](interval))
}

// SubtractDateInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func SubtractDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractDateIntervalValue(rowInput(input).Value(), interval))
}

// SubtractDateIntervalValue retains the selected aggregate/window phase.
func SubtractDateIntervalValue[S any](input Expression[S, temporal.Date], interval temporal.Interval) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](subtractDateIntervalOperation, codec.LocalDateTime(), operationArg(input), intervalArgument[S](interval))
}

// SubtractDateIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractDateIntervalNullableValue(rowInput(input).Value(), interval))
}

// SubtractDateIntervalNullableValue retains the selected aggregate/window phase.
func SubtractDateIntervalNullableValue[S any](input Expression[S, value.Nullable[temporal.Date]], interval temporal.Interval) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](subtractDateIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), intervalArgument[S](interval))
}

// AddLocalInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func AddLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddLocalIntervalValue(rowInput(input).Value(), interval))
}

// AddLocalIntervalValue retains the selected aggregate/window phase.
func AddLocalIntervalValue[S any](input Expression[S, temporal.LocalDateTime], interval temporal.Interval) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](addLocalIntervalOperation, codec.LocalDateTime(), operationArg(input), intervalArgument[S](interval))
}

// AddLocalIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddLocalIntervalNullableValue(rowInput(input).Value(), interval))
}

// AddLocalIntervalNullableValue retains the selected aggregate/window phase.
func AddLocalIntervalNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](addLocalIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), intervalArgument[S](interval))
}

// SubtractLocalInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
func SubtractLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractLocalIntervalValue(rowInput(input).Value(), interval))
}

// SubtractLocalIntervalValue retains the selected aggregate/window phase.
func SubtractLocalIntervalValue[S any](input Expression[S, temporal.LocalDateTime], interval temporal.Interval) Expression[S, temporal.LocalDateTime] {
	return operationValue[S](subtractLocalIntervalOperation, codec.LocalDateTime(), operationArg(input), intervalArgument[S](interval))
}

// SubtractLocalIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractLocalIntervalNullableValue(rowInput(input).Value(), interval))
}

// SubtractLocalIntervalNullableValue retains the selected aggregate/window phase.
func SubtractLocalIntervalNullableValue[S any](input Expression[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Expression[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](subtractLocalIntervalOperation, codec.Nullable(codec.LocalDateTime()), operationArg(input), intervalArgument[S](interval))
}

// AddInstantInterval adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
func AddInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(AddInstantIntervalValue(rowInput(input).Value(), interval, zone))
}

// AddInstantIntervalValue retains the selected aggregate/window phase.
func AddInstantIntervalValue[S any, V instantValue](input Expression[S, V], interval temporal.Interval, zone TimeZone) Expression[S, V] {
	return operationValue[S](addInstantIntervalOperation, input.codec, operationArg(input), intervalArgument[S](interval), zoneArgument[S](zone))
}

// AddInstantIntervalNullable adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func AddInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AddInstantIntervalNullableValue(rowInput(input).Value(), interval, zone))
}

// AddInstantIntervalNullableValue retains the selected aggregate/window phase.
func AddInstantIntervalNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Expression[S, value.Nullable[V]] {
	return operationValue[S](addInstantIntervalOperation, input.codec, operationArg(input), intervalArgument[S](interval), zoneArgument[S](zone))
}

// SubtractInstantInterval subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
func SubtractInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(SubtractInstantIntervalValue(rowInput(input).Value(), interval, zone))
}

// SubtractInstantIntervalValue retains the selected aggregate/window phase.
func SubtractInstantIntervalValue[S any, V instantValue](input Expression[S, V], interval temporal.Interval, zone TimeZone) Expression[S, V] {
	return operationValue[S](subtractInstantIntervalOperation, input.codec, operationArg(input), intervalArgument[S](interval), zoneArgument[S](zone))
}

// SubtractInstantIntervalNullable subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
func SubtractInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(SubtractInstantIntervalNullableValue(rowInput(input).Value(), interval, zone))
}

// SubtractInstantIntervalNullableValue retains the selected aggregate/window phase.
func SubtractInstantIntervalNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Expression[S, value.Nullable[V]] {
	return operationValue[S](subtractInstantIntervalOperation, input.codec, operationArg(input), intervalArgument[S](interval), zoneArgument[S](zone))
}

// AddClockElapsed adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required.
func AddClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(AddClockElapsedValue(rowInput(input).Value(), duration))
}

// AddClockElapsedValue retains the selected aggregate/window phase.
func AddClockElapsedValue[S any](input Expression[S, temporal.Time], duration time.Duration) Expression[S, temporal.Time] {
	return operationValue[S](addClockElapsedOperation, codec.WallTime(), operationArg(input), elapsedArgument[S](duration))
}

// AddClockElapsedNullable adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
func AddClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(AddClockElapsedNullableValue(rowInput(input).Value(), duration))
}

// AddClockElapsedNullableValue retains the selected aggregate/window phase.
func AddClockElapsedNullableValue[S any](input Expression[S, value.Nullable[temporal.Time]], duration time.Duration) Expression[S, value.Nullable[temporal.Time]] {
	return operationValue[S](addClockElapsedOperation, codec.Nullable(codec.WallTime()), operationArg(input), elapsedArgument[S](duration))
}

// SubtractClockElapsed subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required.
func SubtractClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(SubtractClockElapsedValue(rowInput(input).Value(), duration))
}

// SubtractClockElapsedValue retains the selected aggregate/window phase.
func SubtractClockElapsedValue[S any](input Expression[S, temporal.Time], duration time.Duration) Expression[S, temporal.Time] {
	return operationValue[S](subtractClockElapsedOperation, codec.WallTime(), operationArg(input), elapsedArgument[S](duration))
}

// SubtractClockElapsedNullable subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
func SubtractClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(SubtractClockElapsedNullableValue(rowInput(input).Value(), duration))
}

// SubtractClockElapsedNullableValue retains the selected aggregate/window phase.
func SubtractClockElapsedNullableValue[S any](input Expression[S, value.Nullable[temporal.Time]], duration time.Duration) Expression[S, value.Nullable[temporal.Time]] {
	return operationValue[S](subtractClockElapsedOperation, codec.Nullable(codec.WallTime()), operationArg(input), elapsedArgument[S](duration))
}

// TransactionTime returns PostgreSQL's transaction start instant. It is stable
// throughout a transaction and does not use the application's test clock.
func TransactionTime[S any](source ScopeSource[S]) OrderedRowExpression[S, temporal.DateTime] {
	result := operationValue[S](transactionTimeOperation, codec.DateTime())
	var err error
	if nilDescriptor(source) {
		err = fault.New(fault.Invalid, "database time requires a source scope")
	} else {
		err = source.scopeSource().err
	}
	if err != nil {
		node := result.node.(operationNode)
		node.err = err
		result.node = node
	}
	return orderedRow(result)
}
