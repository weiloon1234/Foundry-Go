package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Year extracts year from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Year[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(YearValue(rowInput(input).Value()))
}

// YearValue retains the selected aggregate/window phase.
func YearValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractYearOperation, codec.Signed[int64](), operationArg(input))
}

// YearNullable propagates NULL without changing its input scope.
func YearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(YearNullableValue(rowInput(input).Value()))
}

// YearNullableValue is the nullable selected-value counterpart.
func YearNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractYearOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Month extracts month from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Month[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MonthValue(rowInput(input).Value()))
}

// MonthValue retains the selected aggregate/window phase.
func MonthValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractMonthOperation, codec.Signed[int64](), operationArg(input))
}

// MonthNullable propagates NULL without changing its input scope.
func MonthNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MonthNullableValue(rowInput(input).Value()))
}

// MonthNullableValue is the nullable selected-value counterpart.
func MonthNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractMonthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Day extracts day from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Day[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayValue(rowInput(input).Value()))
}

// DayValue retains the selected aggregate/window phase.
func DayValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractDayOperation, codec.Signed[int64](), operationArg(input))
}

// DayNullable propagates NULL without changing its input scope.
func DayNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayNullableValue(rowInput(input).Value()))
}

// DayNullableValue is the nullable selected-value counterpart.
func DayNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractDayOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// DayOfWeek extracts dow from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func DayOfWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayOfWeekValue(rowInput(input).Value()))
}

// DayOfWeekValue retains the selected aggregate/window phase.
func DayOfWeekValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractDayOfWeekOperation, codec.Signed[int64](), operationArg(input))
}

// DayOfWeekNullable propagates NULL without changing its input scope.
func DayOfWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayOfWeekNullableValue(rowInput(input).Value()))
}

// DayOfWeekNullableValue is the nullable selected-value counterpart.
func DayOfWeekNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractDayOfWeekOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// ISODayOfWeek extracts isodow from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func ISODayOfWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISODayOfWeekValue(rowInput(input).Value()))
}

// ISODayOfWeekValue retains the selected aggregate/window phase.
func ISODayOfWeekValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractISODayOfWeekOperation, codec.Signed[int64](), operationArg(input))
}

// ISODayOfWeekNullable propagates NULL without changing its input scope.
func ISODayOfWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISODayOfWeekNullableValue(rowInput(input).Value()))
}

// ISODayOfWeekNullableValue is the nullable selected-value counterpart.
func ISODayOfWeekNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractISODayOfWeekOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// DayOfYear extracts doy from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func DayOfYear[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayOfYearValue(rowInput(input).Value()))
}

// DayOfYearValue retains the selected aggregate/window phase.
func DayOfYearValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractDayOfYearOperation, codec.Signed[int64](), operationArg(input))
}

// DayOfYearNullable propagates NULL without changing its input scope.
func DayOfYearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayOfYearNullableValue(rowInput(input).Value()))
}

// DayOfYearNullableValue is the nullable selected-value counterpart.
func DayOfYearNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractDayOfYearOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// ISOWeek extracts week from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func ISOWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISOWeekValue(rowInput(input).Value()))
}

// ISOWeekValue retains the selected aggregate/window phase.
func ISOWeekValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractISOWeekOperation, codec.Signed[int64](), operationArg(input))
}

// ISOWeekNullable propagates NULL without changing its input scope.
func ISOWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISOWeekNullableValue(rowInput(input).Value()))
}

// ISOWeekNullableValue is the nullable selected-value counterpart.
func ISOWeekNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractISOWeekOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// ISOYear extracts isoyear from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func ISOYear[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISOYearValue(rowInput(input).Value()))
}

// ISOYearValue retains the selected aggregate/window phase.
func ISOYearValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractISOYearOperation, codec.Signed[int64](), operationArg(input))
}

// ISOYearNullable propagates NULL without changing its input scope.
func ISOYearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISOYearNullableValue(rowInput(input).Value()))
}

// ISOYearNullableValue is the nullable selected-value counterpart.
func ISOYearNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractISOYearOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Quarter extracts quarter from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Quarter[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(QuarterValue(rowInput(input).Value()))
}

// QuarterValue retains the selected aggregate/window phase.
func QuarterValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractQuarterOperation, codec.Signed[int64](), operationArg(input))
}

// QuarterNullable propagates NULL without changing its input scope.
func QuarterNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(QuarterNullableValue(rowInput(input).Value()))
}

// QuarterNullableValue is the nullable selected-value counterpart.
func QuarterNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractQuarterOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Century extracts century from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Century[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(CenturyValue(rowInput(input).Value()))
}

// CenturyValue retains the selected aggregate/window phase.
func CenturyValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractCenturyOperation, codec.Signed[int64](), operationArg(input))
}

// CenturyNullable propagates NULL without changing its input scope.
func CenturyNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(CenturyNullableValue(rowInput(input).Value()))
}

// CenturyNullableValue is the nullable selected-value counterpart.
func CenturyNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractCenturyOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Decade extracts decade from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Decade[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DecadeValue(rowInput(input).Value()))
}

// DecadeValue retains the selected aggregate/window phase.
func DecadeValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractDecadeOperation, codec.Signed[int64](), operationArg(input))
}

// DecadeNullable propagates NULL without changing its input scope.
func DecadeNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DecadeNullableValue(rowInput(input).Value()))
}

// DecadeNullableValue is the nullable selected-value counterpart.
func DecadeNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractDecadeOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Millennium extracts millennium from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
func Millennium[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MillenniumValue(rowInput(input).Value()))
}

// MillenniumValue retains the selected aggregate/window phase.
func MillenniumValue[S any, V calendarValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractMillenniumOperation, codec.Signed[int64](), operationArg(input))
}

// MillenniumNullable propagates NULL without changing its input scope.
func MillenniumNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MillenniumNullableValue(rowInput(input).Value()))
}

// MillenniumNullableValue is the nullable selected-value counterpart.
func MillenniumNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractMillenniumOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// JulianDay returns the Julian day, including a fractional day for local
// date-times. Use LocalAt to select an instant's timezone first.
func JulianDay[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(JulianDayValue(rowInput(input).Value()))
}

// JulianDayValue retains the selected aggregate/window phase.
func JulianDayValue[S any, V calendarValue](input Expression[S, V]) Expression[S, decimal.Decimal] {
	return operationValue[S](extractJulianDayOperation, codec.Decimal(), operationArg(input))
}

// JulianDayNullable propagates NULL without changing its input scope.
func JulianDayNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(JulianDayNullableValue(rowInput(input).Value()))
}

// JulianDayNullableValue is the nullable selected-value counterpart.
func JulianDayNullableValue[S any, V calendarValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](extractJulianDayOperation, codec.Nullable(codec.Decimal()), operationArg(input))
}

// Hour extracts the hour from a time or local date-time.
// Use LocalAt to select an instant's timezone first.
func Hour[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(HourValue(rowInput(input).Value()))
}

// HourValue retains the selected aggregate/window phase.
func HourValue[S any, V clockValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractHourOperation, codec.Signed[int64](), operationArg(input))
}

// HourNullable propagates NULL without changing its input scope.
func HourNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(HourNullableValue(rowInput(input).Value()))
}

// HourNullableValue is the nullable selected-value counterpart.
func HourNullableValue[S any, V clockValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractHourOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Minute extracts the minute from a time or local date-time.
// Use LocalAt to select an instant's timezone first.
func Minute[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MinuteValue(rowInput(input).Value()))
}

// MinuteValue retains the selected aggregate/window phase.
func MinuteValue[S any, V clockValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractMinuteOperation, codec.Signed[int64](), operationArg(input))
}

// MinuteNullable propagates NULL without changing its input scope.
func MinuteNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MinuteNullableValue(rowInput(input).Value()))
}

// MinuteNullableValue is the nullable selected-value counterpart.
func MinuteNullableValue[S any, V clockValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractMinuteOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// Second returns the exact seconds component, including fractional seconds,
// from a time or local date-time. Use LocalAt to select an instant's timezone first.
func Second[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(SecondValue(rowInput(input).Value()))
}

// SecondValue retains the selected aggregate/window phase.
func SecondValue[S any, V clockValue](input Expression[S, V]) Expression[S, decimal.Decimal] {
	return operationValue[S](extractSecondOperation, codec.Decimal(), operationArg(input))
}

// SecondNullable propagates NULL without changing its input scope.
func SecondNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(SecondNullableValue(rowInput(input).Value()))
}

// SecondNullableValue is the nullable selected-value counterpart.
func SecondNullableValue[S any, V clockValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](extractSecondOperation, codec.Nullable(codec.Decimal()), operationArg(input))
}

// Milliseconds returns the seconds component multiplied by 1000, including
// fractional milliseconds. Use LocalAt to select an instant's timezone first.
func Milliseconds[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(MillisecondsValue(rowInput(input).Value()))
}

// MillisecondsValue retains the selected aggregate/window phase.
func MillisecondsValue[S any, V clockValue](input Expression[S, V]) Expression[S, decimal.Decimal] {
	return operationValue[S](extractMillisecondsOperation, codec.Decimal(), operationArg(input))
}

// MillisecondsNullable propagates NULL without changing its input scope.
func MillisecondsNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(MillisecondsNullableValue(rowInput(input).Value()))
}

// MillisecondsNullableValue is the nullable selected-value counterpart.
func MillisecondsNullableValue[S any, V clockValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](extractMillisecondsOperation, codec.Nullable(codec.Decimal()), operationArg(input))
}

// Microseconds returns the whole seconds component multiplied by 1000000 plus
// its fractional microseconds. Use LocalAt to select an instant's timezone first.
func Microseconds[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MicrosecondsValue(rowInput(input).Value()))
}

// MicrosecondsValue retains the selected aggregate/window phase.
func MicrosecondsValue[S any, V clockValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractMicrosecondsOperation, codec.Signed[int64](), operationArg(input))
}

// MicrosecondsNullable propagates NULL without changing its input scope.
func MicrosecondsNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MicrosecondsNullableValue(rowInput(input).Value()))
}

// MicrosecondsNullableValue is the nullable selected-value counterpart.
func MicrosecondsNullableValue[S any, V clockValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractMicrosecondsOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// UnixSeconds returns exact decimal seconds since the Unix epoch for an instant.
func UnixSeconds[S any, V instantValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(UnixSecondsValue(rowInput(input).Value()))
}

// UnixSecondsValue retains the selected aggregate/window phase.
func UnixSecondsValue[S any, V instantValue](input Expression[S, V]) Expression[S, decimal.Decimal] {
	return operationValue[S](extractUnixSecondsOperation, codec.Decimal(), operationArg(input))
}

// UnixSecondsNullable propagates NULL without changing its input scope.
func UnixSecondsNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(UnixSecondsNullableValue(rowInput(input).Value()))
}

// UnixSecondsNullableValue is the nullable selected-value counterpart.
func UnixSecondsNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](extractUnixSecondsOperation, codec.Nullable(codec.Decimal()), operationArg(input))
}

// UnixMilliseconds returns integer milliseconds since the Unix epoch.
// Fractional milliseconds round down, matching time.Time.UnixMilli before epoch.
func UnixMilliseconds[S any, V instantValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(UnixMillisecondsValue(rowInput(input).Value()))
}

// UnixMillisecondsValue retains the selected aggregate/window phase.
func UnixMillisecondsValue[S any, V instantValue](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](extractUnixMillisecondsOperation, codec.Signed[int64](), operationArg(input))
}

// UnixMillisecondsNullable propagates NULL without changing its input scope.
func UnixMillisecondsNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(UnixMillisecondsNullableValue(rowInput(input).Value()))
}

// UnixMillisecondsNullableValue is the nullable selected-value counterpart.
func UnixMillisecondsNullableValue[S any, V instantValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](extractUnixMillisecondsOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}
