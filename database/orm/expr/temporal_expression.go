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
//
// Example: expr.TruncateDate(input, unit)
func TruncateDate[S any](input RowValue[S, temporal.Date], unit DateUnit) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(TruncateDateValue(AsSelected(rowInput(input)), unit))
}

// TruncateDateValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateDateValue(input, unit)
func TruncateDateValue[S any](input Selected[S, temporal.Date], unit DateUnit) Selected[S, temporal.Date] {
	return operationValue[S](querycore.TruncateDateOperation, columnCodec(codec.Date()), unitArgument(temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateDateNullable truncates a calendar date without introducing a timezone. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.TruncateDateNullable(input, unit)
func TruncateDateNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], unit DateUnit) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(TruncateDateNullableValue(AsSelected(rowInput(input)), unit))
}

// TruncateDateNullableValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateDateNullableValue(input, unit)
func TruncateDateNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], unit DateUnit) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.TruncateDateOperation, columnCodec(codec.Nullable(codec.Date())), unitArgument(temporalUnit(unit), unitDay), operationArg(input))
}

// TruncateLocal truncates local wall components without interpreting them as an instant.
//
// Example: expr.TruncateLocal(input, unit)
func TruncateLocal[S any](input RowValue[S, temporal.LocalDateTime], unit TimestampUnit) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(TruncateLocalValue(AsSelected(rowInput(input)), unit))
}

// TruncateLocalValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateLocalValue(input, unit)
func TruncateLocalValue[S any](input Selected[S, temporal.LocalDateTime], unit TimestampUnit) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.TruncateLocalOperation, columnCodec(codec.LocalDateTime()), unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateLocalNullable truncates local wall components without interpreting them as an instant. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.TruncateLocalNullable(input, unit)
func TruncateLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(TruncateLocalNullableValue(AsSelected(rowInput(input)), unit))
}

// TruncateLocalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateLocalNullableValue(input, unit)
func TruncateLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], unit TimestampUnit) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.TruncateLocalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input))
}

// TruncateInstant truncates an instant in the explicit zone while preserving its Go type.
//
// Example: expr.TruncateInstant(input, unit, zone)
func TruncateInstant[S any, V instantValue](input RowValue[S, V], unit TimestampUnit, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(TruncateInstantValue(AsSelected(rowInput(input)), unit, zone))
}

// TruncateInstantValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateInstantValue(input, unit, zone)
func TruncateInstantValue[S any, V instantValue](input Selected[S, V], unit TimestampUnit, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.TruncateInstantOperation, input.data.column, unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument(zone))
}

// TruncateInstantNullable truncates an instant in the explicit zone while preserving its Go type. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.TruncateInstantNullable(input, unit, zone)
func TruncateInstantNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(TruncateInstantNullableValue(AsSelected(rowInput(input)), unit, zone))
}

// TruncateInstantNullableValue retains the selected aggregate/window phase.
//
// Example: expr.TruncateInstantNullableValue(input, unit, zone)
func TruncateInstantNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], unit TimestampUnit, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.TruncateInstantOperation, input.data.column, unitArgument(temporalUnit(unit), unitMicrosecond), operationArg(input), zoneArgument(zone))
}

// FromUnixMillis converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding.
//
// Example: expr.FromUnixMillis(input)
func FromUnixMillis[S any, V integerNumber](input RowValue[S, V]) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(FromUnixMillisValue(AsSelected(rowInput(input))))
}

// FromUnixMillisValue retains the selected aggregate/window phase.
//
// Example: expr.FromUnixMillisValue(input)
func FromUnixMillisValue[S any, V integerNumber](input Selected[S, V]) Selected[S, temporal.DateTime] {
	return operationValue[S](querycore.FromUnixMillisOperation, columnCodec(codec.DateTime()), operationArg(input))
}

// FromUnixMillisNullable converts exact integer Unix milliseconds to a UTC instant without floating-point epoch rounding. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.FromUnixMillisNullable(input)
func FromUnixMillisNullable[S any, V integerNumber](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(FromUnixMillisNullableValue(AsSelected(rowInput(input))))
}

// FromUnixMillisNullableValue retains the selected aggregate/window phase.
//
// Example: expr.FromUnixMillisNullableValue(input)
func FromUnixMillisNullableValue[S any, V integerNumber](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](querycore.FromUnixMillisOperation, columnCodec(codec.Nullable(codec.DateTime())), operationArg(input))
}

// LocalAt converts an instant to wall components in the explicitly selected zone.
//
// Example: expr.LocalAt(input, zone)
func LocalAt[S any, V instantValue](input RowValue[S, V], zone TimeZone) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(LocalAtValue(AsSelected(rowInput(input)), zone))
}

// LocalAtValue retains the selected aggregate/window phase.
//
// Example: expr.LocalAtValue(input, zone)
func LocalAtValue[S any, V instantValue](input Selected[S, V], zone TimeZone) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.LocalAtOperation, columnCodec(codec.LocalDateTime()), zoneArgument(zone), operationArg(input))
}

// LocalAtNullable converts an instant to wall components in the explicitly selected zone. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.LocalAtNullable(input, zone)
func LocalAtNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], zone TimeZone) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(LocalAtNullableValue(AsSelected(rowInput(input)), zone))
}

// LocalAtNullableValue retains the selected aggregate/window phase.
//
// Example: expr.LocalAtNullableValue(input, zone)
func LocalAtNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], zone TimeZone) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.LocalAtOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), zoneArgument(zone), operationArg(input))
}

// ResolveLocal uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In.
//
// Example: expr.ResolveLocal(input, zone, policy)
func ResolveLocal[S any](input RowValue[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) OrderedRowExpression[S, temporal.DateTime] {
	return orderedRow(ResolveLocalValue(AsSelected(rowInput(input)), zone, policy))
}

// ResolveLocalValue retains the selected aggregate/window phase.
//
// Example: expr.ResolveLocalValue(input, zone, policy)
func ResolveLocalValue[S any](input Selected[S, temporal.LocalDateTime], zone TimeZone, policy LocalResolution) Selected[S, temporal.DateTime] {
	return operationValue[S](querycore.ResolveLocalOperation, columnCodec(codec.DateTime()), resolutionArgument(zone, policy), operationArg(input))
}

// ResolveLocalNullable uses the explicitly selected PostgreSQL DST resolution policy. It differs from the uniqueness checks in temporal.LocalDateTime.In. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.ResolveLocalNullable(input, zone, policy)
func ResolveLocalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) NullableOrderedRowExpression[S, temporal.DateTime] {
	return nullableOrderedRow(ResolveLocalNullableValue(AsSelected(rowInput(input)), zone, policy))
}

// ResolveLocalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.ResolveLocalNullableValue(input, zone, policy)
func ResolveLocalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], zone TimeZone, policy LocalResolution) Selected[S, value.Nullable[temporal.DateTime]] {
	return operationValue[S](querycore.ResolveLocalOperation, columnCodec(codec.Nullable(codec.DateTime())), resolutionArgument(zone, policy), operationArg(input))
}

// DateOf selects the calendar date from local wall components.
//
// Example: expr.DateOf(input)
func DateOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(DateOfValue(AsSelected(rowInput(input))))
}

// DateOfValue retains the selected aggregate/window phase.
//
// Example: expr.DateOfValue(input)
func DateOfValue[S any](input Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Date] {
	return operationValue[S](querycore.DateOfOperation, columnCodec(codec.Date()), operationArg(input))
}

// DateOfNullable selects the calendar date from local wall components. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.DateOfNullable(input)
func DateOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(DateOfNullableValue(AsSelected(rowInput(input))))
}

// DateOfNullableValue retains the selected aggregate/window phase.
//
// Example: expr.DateOfNullableValue(input)
func DateOfNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.DateOfOperation, columnCodec(codec.Nullable(codec.Date())), operationArg(input))
}

// TimeOf selects the time of day from local wall components.
//
// Example: expr.TimeOf(input)
func TimeOf[S any](input RowValue[S, temporal.LocalDateTime]) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(TimeOfValue(AsSelected(rowInput(input))))
}

// TimeOfValue retains the selected aggregate/window phase.
//
// Example: expr.TimeOfValue(input)
func TimeOfValue[S any](input Selected[S, temporal.LocalDateTime]) Selected[S, temporal.Time] {
	return operationValue[S](querycore.TimeOfOperation, columnCodec(codec.WallTime()), operationArg(input))
}

// TimeOfNullable selects the time of day from local wall components. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.TimeOfNullable(input)
func TimeOfNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]]) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(TimeOfNullableValue(AsSelected(rowInput(input))))
}

// TimeOfNullableValue retains the selected aggregate/window phase.
//
// Example: expr.TimeOfNullableValue(input)
func TimeOfNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]]) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.TimeOfOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input))
}

// CombineDateTime combines a calendar date and time of day without choosing a timezone.
//
// Example: expr.CombineDateTime(date, clock)
func CombineDateTime[S any](date RowValue[S, temporal.Date], clock RowValue[S, temporal.Time]) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(CombineDateTimeValue(AsSelected(rowInput(date)), AsSelected(rowInput(clock))))
}

// CombineDateTimeValue retains the selected aggregate/window phase.
//
// Example: expr.CombineDateTimeValue(date, clock)
func CombineDateTimeValue[S any](date Selected[S, temporal.Date], clock Selected[S, temporal.Time]) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.CombineDateTimeOperation, columnCodec(codec.LocalDateTime()), operationArg(date), operationArg(clock))
}

// CombineDateTimeNullable combines a calendar date and time of day without choosing a timezone. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.CombineDateTimeNullable(date, clock)
func CombineDateTimeNullable[S any](date RowValue[S, value.Nullable[temporal.Date]], clock RowValue[S, value.Nullable[temporal.Time]]) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(CombineDateTimeNullableValue(AsSelected(rowInput(date)), AsSelected(rowInput(clock))))
}

// CombineDateTimeNullableValue retains the selected aggregate/window phase.
//
// Example: expr.CombineDateTimeNullableValue(date, clock)
func CombineDateTimeNullableValue[S any](date Selected[S, value.Nullable[temporal.Date]], clock Selected[S, value.Nullable[temporal.Time]]) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.CombineDateTimeOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(date), operationArg(clock))
}

// AddDateDays adds signed calendar days and retains the date-only result.
//
// Example: expr.AddDateDays(input, days)
func AddDateDays[S any](input RowValue[S, temporal.Date], days int32) OrderedRowExpression[S, temporal.Date] {
	return orderedRow(AddDateDaysValue(AsSelected(rowInput(input)), days))
}

// AddDateDaysValue retains the selected aggregate/window phase.
//
// Example: expr.AddDateDaysValue(input, days)
func AddDateDaysValue[S any](input Selected[S, temporal.Date], days int32) Selected[S, temporal.Date] {
	return operationValue[S](querycore.AddDateDaysOperation, columnCodec(codec.Date()), operationArg(input), integerArgument(days))
}

// AddDateDaysNullable adds signed calendar days and retains the date-only result. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.AddDateDaysNullable(input, days)
func AddDateDaysNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], days int32) NullableOrderedRowExpression[S, temporal.Date] {
	return nullableOrderedRow(AddDateDaysNullableValue(AsSelected(rowInput(input)), days))
}

// AddDateDaysNullableValue retains the selected aggregate/window phase.
//
// Example: expr.AddDateDaysNullableValue(input, days)
func AddDateDaysNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], days int32) Selected[S, value.Nullable[temporal.Date]] {
	return operationValue[S](querycore.AddDateDaysOperation, columnCodec(codec.Nullable(codec.Date())), operationArg(input), integerArgument(days))
}

// DateDifference returns the signed number of calendar days from right to left.
//
// Example: expr.DateDifference(left, right)
func DateDifference[S any](left RowValue[S, temporal.Date], right RowValue[S, temporal.Date]) OrderedRowExpression[S, int64] {
	return orderedRow(DateDifferenceValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// DateDifferenceValue retains the selected aggregate/window phase.
//
// Example: expr.DateDifferenceValue(left, right)
func DateDifferenceValue[S any](left Selected[S, temporal.Date], right Selected[S, temporal.Date]) Selected[S, int64] {
	return operationValue[S](querycore.DateDifferenceOperation, columnCodec(codec.Signed[int64]()), operationArg(left), operationArg(right))
}

// DateDifferenceNullable returns the signed number of calendar days from right to left. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.DateDifferenceNullable(left, right)
func DateDifferenceNullable[S any](left RowValue[S, value.Nullable[temporal.Date]], right RowValue[S, value.Nullable[temporal.Date]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DateDifferenceNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// DateDifferenceNullableValue retains the selected aggregate/window phase.
//
// Example: expr.DateDifferenceNullableValue(left, right)
func DateDifferenceNullableValue[S any](left Selected[S, value.Nullable[temporal.Date]], right Selected[S, value.Nullable[temporal.Date]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.DateDifferenceOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(left), operationArg(right))
}

// AddDateInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
//
// Example: expr.AddDateInterval(input, interval)
func AddDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddDateIntervalValue(AsSelected(rowInput(input)), interval))
}

// AddDateIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.AddDateIntervalValue(input, interval)
func AddDateIntervalValue[S any](input Selected[S, temporal.Date], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// AddDateIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.AddDateIntervalNullable(input, interval)
func AddDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddDateIntervalNullableValue(AsSelected(rowInput(input)), interval))
}

// AddDateIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.AddDateIntervalNullableValue(input, interval)
func AddDateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// SubtractDateInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
//
// Example: expr.SubtractDateInterval(input, interval)
func SubtractDateInterval[S any](input RowValue[S, temporal.Date], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractDateIntervalValue(AsSelected(rowInput(input)), interval))
}

// SubtractDateIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractDateIntervalValue(input, interval)
func SubtractDateIntervalValue[S any](input Selected[S, temporal.Date], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.SubtractDateIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// SubtractDateIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.SubtractDateIntervalNullable(input, interval)
func SubtractDateIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.Date]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractDateIntervalNullableValue(AsSelected(rowInput(input)), interval))
}

// SubtractDateIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractDateIntervalNullableValue(input, interval)
func SubtractDateIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.Date]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.SubtractDateIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// AddLocalInterval adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
//
// Example: expr.AddLocalInterval(input, interval)
func AddLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(AddLocalIntervalValue(AsSelected(rowInput(input)), interval))
}

// AddLocalIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.AddLocalIntervalValue(input, interval)
func AddLocalIntervalValue[S any](input Selected[S, temporal.LocalDateTime], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// AddLocalIntervalNullable adds signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.AddLocalIntervalNullable(input, interval)
func AddLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(AddLocalIntervalNullableValue(AsSelected(rowInput(input)), interval))
}

// AddLocalIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.AddLocalIntervalNullableValue(input, interval)
func AddLocalIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.AddLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// SubtractLocalInterval subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors.
//
// Example: expr.SubtractLocalInterval(input, interval)
func SubtractLocalInterval[S any](input RowValue[S, temporal.LocalDateTime], interval temporal.Interval) OrderedRowExpression[S, temporal.LocalDateTime] {
	return orderedRow(SubtractLocalIntervalValue(AsSelected(rowInput(input)), interval))
}

// SubtractLocalIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractLocalIntervalValue(input, interval)
func SubtractLocalIntervalValue[S any](input Selected[S, temporal.LocalDateTime], interval temporal.Interval) Selected[S, temporal.LocalDateTime] {
	return operationValue[S](querycore.SubtractLocalIntervalOperation, columnCodec(codec.LocalDateTime()), operationArg(input), intervalArgument(interval))
}

// SubtractLocalIntervalNullable subtracts signed calendar and elapsed components. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.SubtractLocalIntervalNullable(input, interval)
func SubtractLocalIntervalNullable[S any](input RowValue[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) NullableOrderedRowExpression[S, temporal.LocalDateTime] {
	return nullableOrderedRow(SubtractLocalIntervalNullableValue(AsSelected(rowInput(input)), interval))
}

// SubtractLocalIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractLocalIntervalNullableValue(input, interval)
func SubtractLocalIntervalNullableValue[S any](input Selected[S, value.Nullable[temporal.LocalDateTime]], interval temporal.Interval) Selected[S, value.Nullable[temporal.LocalDateTime]] {
	return operationValue[S](querycore.SubtractLocalIntervalOperation, columnCodec(codec.Nullable(codec.LocalDateTime())), operationArg(input), intervalArgument(interval))
}

// AddInstantInterval adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
//
// Example: expr.AddInstantInterval(input, interval, zone)
func AddInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(AddInstantIntervalValue(AsSelected(rowInput(input)), interval, zone))
}

// AddInstantIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.AddInstantIntervalValue(input, interval, zone)
func AddInstantIntervalValue[S any, V instantValue](input Selected[S, V], interval temporal.Interval, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// AddInstantIntervalNullable adds signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.AddInstantIntervalNullable(input, interval, zone)
func AddInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AddInstantIntervalNullableValue(AsSelected(rowInput(input)), interval, zone))
}

// AddInstantIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.AddInstantIntervalNullableValue(input, interval, zone)
func AddInstantIntervalNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AddInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// SubtractInstantInterval subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors.
//
// Example: expr.SubtractInstantInterval(input, interval, zone)
func SubtractInstantInterval[S any, V instantValue](input RowValue[S, V], interval temporal.Interval, zone TimeZone) OrderedRowExpression[S, V] {
	return orderedRow(SubtractInstantIntervalValue(AsSelected(rowInput(input)), interval, zone))
}

// SubtractInstantIntervalValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractInstantIntervalValue(input, interval, zone)
func SubtractInstantIntervalValue[S any, V instantValue](input Selected[S, V], interval temporal.Interval, zone TimeZone) Selected[S, V] {
	return operationValue[S](querycore.SubtractInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// SubtractInstantIntervalNullable subtracts signed calendar and elapsed components in the explicit timezone. Database overflow and out-of-contract results remain errors. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.SubtractInstantIntervalNullable(input, interval, zone)
func SubtractInstantIntervalNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(SubtractInstantIntervalNullableValue(AsSelected(rowInput(input)), interval, zone))
}

// SubtractInstantIntervalNullableValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractInstantIntervalNullableValue(input, interval, zone)
func SubtractInstantIntervalNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]], interval temporal.Interval, zone TimeZone) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.SubtractInstantIntervalOperation, input.data.column, operationArg(input), intervalArgument(interval), zoneArgument(zone))
}

// AddClockElapsed adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required.
//
// Example: expr.AddClockElapsed(input, duration)
func AddClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(AddClockElapsedValue(AsSelected(rowInput(input)), duration))
}

// AddClockElapsedValue retains the selected aggregate/window phase.
//
// Example: expr.AddClockElapsedValue(input, duration)
func AddClockElapsedValue[S any](input Selected[S, temporal.Time], duration time.Duration) Selected[S, temporal.Time] {
	return operationValue[S](querycore.AddClockElapsedOperation, columnCodec(codec.WallTime()), operationArg(input), elapsedArgument(duration))
}

// AddClockElapsedNullable adds elapsed time to a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.AddClockElapsedNullable(input, duration)
func AddClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(AddClockElapsedNullableValue(AsSelected(rowInput(input)), duration))
}

// AddClockElapsedNullableValue retains the selected aggregate/window phase.
//
// Example: expr.AddClockElapsedNullableValue(input, duration)
func AddClockElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Time]], duration time.Duration) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.AddClockElapsedOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input), elapsedArgument(duration))
}

// SubtractClockElapsed subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required.
//
// Example: expr.SubtractClockElapsed(input, duration)
func SubtractClockElapsed[S any](input RowValue[S, temporal.Time], duration time.Duration) OrderedRowExpression[S, temporal.Time] {
	return orderedRow(SubtractClockElapsedValue(AsSelected(rowInput(input)), duration))
}

// SubtractClockElapsedValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractClockElapsedValue(input, duration)
func SubtractClockElapsedValue[S any](input Selected[S, temporal.Time], duration time.Duration) Selected[S, temporal.Time] {
	return operationValue[S](querycore.SubtractClockElapsedOperation, columnCodec(codec.WallTime()), operationArg(input), elapsedArgument(duration))
}

// SubtractClockElapsedNullable subtracts elapsed time from a time of day, wrapping at midnight. Whole microseconds are required. SQL NULL propagates; widen non-null operands explicitly.
//
// Example: expr.SubtractClockElapsedNullable(input, duration)
func SubtractClockElapsedNullable[S any](input RowValue[S, value.Nullable[temporal.Time]], duration time.Duration) NullableOrderedRowExpression[S, temporal.Time] {
	return nullableOrderedRow(SubtractClockElapsedNullableValue(AsSelected(rowInput(input)), duration))
}

// SubtractClockElapsedNullableValue retains the selected aggregate/window phase.
//
// Example: expr.SubtractClockElapsedNullableValue(input, duration)
func SubtractClockElapsedNullableValue[S any](input Selected[S, value.Nullable[temporal.Time]], duration time.Duration) Selected[S, value.Nullable[temporal.Time]] {
	return operationValue[S](querycore.SubtractClockElapsedOperation, columnCodec(codec.Nullable(codec.WallTime())), operationArg(input), elapsedArgument(duration))
}

// TransactionTime returns PostgreSQL's transaction start instant. It is stable
// throughout a transaction and does not use the application's test clock.
//
// Example: expr.TransactionTime(scope)
func TransactionTime[S, M any](scope Scope[S, M]) OrderedRowExpression[S, temporal.DateTime] {
	result := operationValue[S](querycore.TransactionTimeOperation, columnCodec(codec.DateTime()))
	result.data.core.Refs = []ormcore.Reference{scope.core.Reference, {Scope: reflect.TypeFor[S]()}}
	result.data.core.Err = scope.core.Err
	if scope.core.Owner == nil || scope.core.Table == "" {
		result.data.core.Err = safeerror.Join("operation failed", result.data.core.Err, ormcore.Invalid("database time requires a source scope"))
	}
	return orderedRow(result)
}
