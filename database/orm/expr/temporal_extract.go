package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Year extracts year from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Year(input)
func Year[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(YearValue(AsSelected(rowInput(input))))
}

// YearValue retains the selected aggregate/window phase.
//
// Example: expr.YearValue(input)
func YearValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractYearOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// YearNullable propagates NULL without changing its input scope.
//
// Example: expr.YearNullable(input)
func YearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(YearNullableValue(AsSelected(rowInput(input))))
}

// YearNullableValue is the nullable selected-value counterpart.
//
// Example: expr.YearNullableValue(input)
func YearNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractYearOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Month extracts month from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Month(input)
func Month[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MonthValue(AsSelected(rowInput(input))))
}

// MonthValue retains the selected aggregate/window phase.
//
// Example: expr.MonthValue(input)
func MonthValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractMonthOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// MonthNullable propagates NULL without changing its input scope.
//
// Example: expr.MonthNullable(input)
func MonthNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MonthNullableValue(AsSelected(rowInput(input))))
}

// MonthNullableValue is the nullable selected-value counterpart.
//
// Example: expr.MonthNullableValue(input)
func MonthNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractMonthOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Day extracts day from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Day(input)
func Day[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayValue(AsSelected(rowInput(input))))
}

// DayValue retains the selected aggregate/window phase.
//
// Example: expr.DayValue(input)
func DayValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractDayOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// DayNullable propagates NULL without changing its input scope.
//
// Example: expr.DayNullable(input)
func DayNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayNullableValue(AsSelected(rowInput(input))))
}

// DayNullableValue is the nullable selected-value counterpart.
//
// Example: expr.DayNullableValue(input)
func DayNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractDayOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// DayOfWeek extracts dow from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.DayOfWeek(input)
func DayOfWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayOfWeekValue(AsSelected(rowInput(input))))
}

// DayOfWeekValue retains the selected aggregate/window phase.
//
// Example: expr.DayOfWeekValue(input)
func DayOfWeekValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractDayOfWeekOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// DayOfWeekNullable propagates NULL without changing its input scope.
//
// Example: expr.DayOfWeekNullable(input)
func DayOfWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayOfWeekNullableValue(AsSelected(rowInput(input))))
}

// DayOfWeekNullableValue is the nullable selected-value counterpart.
//
// Example: expr.DayOfWeekNullableValue(input)
func DayOfWeekNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractDayOfWeekOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// ISODayOfWeek extracts isodow from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.ISODayOfWeek(input)
func ISODayOfWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISODayOfWeekValue(AsSelected(rowInput(input))))
}

// ISODayOfWeekValue retains the selected aggregate/window phase.
//
// Example: expr.ISODayOfWeekValue(input)
func ISODayOfWeekValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractISODayOfWeekOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// ISODayOfWeekNullable propagates NULL without changing its input scope.
//
// Example: expr.ISODayOfWeekNullable(input)
func ISODayOfWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISODayOfWeekNullableValue(AsSelected(rowInput(input))))
}

// ISODayOfWeekNullableValue is the nullable selected-value counterpart.
//
// Example: expr.ISODayOfWeekNullableValue(input)
func ISODayOfWeekNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractISODayOfWeekOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// DayOfYear extracts doy from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.DayOfYear(input)
func DayOfYear[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DayOfYearValue(AsSelected(rowInput(input))))
}

// DayOfYearValue retains the selected aggregate/window phase.
//
// Example: expr.DayOfYearValue(input)
func DayOfYearValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractDayOfYearOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// DayOfYearNullable propagates NULL without changing its input scope.
//
// Example: expr.DayOfYearNullable(input)
func DayOfYearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DayOfYearNullableValue(AsSelected(rowInput(input))))
}

// DayOfYearNullableValue is the nullable selected-value counterpart.
//
// Example: expr.DayOfYearNullableValue(input)
func DayOfYearNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractDayOfYearOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// ISOWeek extracts week from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.ISOWeek(input)
func ISOWeek[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISOWeekValue(AsSelected(rowInput(input))))
}

// ISOWeekValue retains the selected aggregate/window phase.
//
// Example: expr.ISOWeekValue(input)
func ISOWeekValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractISOWeekOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// ISOWeekNullable propagates NULL without changing its input scope.
//
// Example: expr.ISOWeekNullable(input)
func ISOWeekNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISOWeekNullableValue(AsSelected(rowInput(input))))
}

// ISOWeekNullableValue is the nullable selected-value counterpart.
//
// Example: expr.ISOWeekNullableValue(input)
func ISOWeekNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractISOWeekOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// ISOYear extracts isoyear from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.ISOYear(input)
func ISOYear[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(ISOYearValue(AsSelected(rowInput(input))))
}

// ISOYearValue retains the selected aggregate/window phase.
//
// Example: expr.ISOYearValue(input)
func ISOYearValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractISOYearOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// ISOYearNullable propagates NULL without changing its input scope.
//
// Example: expr.ISOYearNullable(input)
func ISOYearNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(ISOYearNullableValue(AsSelected(rowInput(input))))
}

// ISOYearNullableValue is the nullable selected-value counterpart.
//
// Example: expr.ISOYearNullableValue(input)
func ISOYearNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractISOYearOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Quarter extracts quarter from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Quarter(input)
func Quarter[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(QuarterValue(AsSelected(rowInput(input))))
}

// QuarterValue retains the selected aggregate/window phase.
//
// Example: expr.QuarterValue(input)
func QuarterValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractQuarterOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// QuarterNullable propagates NULL without changing its input scope.
//
// Example: expr.QuarterNullable(input)
func QuarterNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(QuarterNullableValue(AsSelected(rowInput(input))))
}

// QuarterNullableValue is the nullable selected-value counterpart.
//
// Example: expr.QuarterNullableValue(input)
func QuarterNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractQuarterOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Century extracts century from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Century(input)
func Century[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(CenturyValue(AsSelected(rowInput(input))))
}

// CenturyValue retains the selected aggregate/window phase.
//
// Example: expr.CenturyValue(input)
func CenturyValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractCenturyOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// CenturyNullable propagates NULL without changing its input scope.
//
// Example: expr.CenturyNullable(input)
func CenturyNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(CenturyNullableValue(AsSelected(rowInput(input))))
}

// CenturyNullableValue is the nullable selected-value counterpart.
//
// Example: expr.CenturyNullableValue(input)
func CenturyNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractCenturyOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Decade extracts decade from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Decade(input)
func Decade[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(DecadeValue(AsSelected(rowInput(input))))
}

// DecadeValue retains the selected aggregate/window phase.
//
// Example: expr.DecadeValue(input)
func DecadeValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractDecadeOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// DecadeNullable propagates NULL without changing its input scope.
//
// Example: expr.DecadeNullable(input)
func DecadeNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(DecadeNullableValue(AsSelected(rowInput(input))))
}

// DecadeNullableValue is the nullable selected-value counterpart.
//
// Example: expr.DecadeNullableValue(input)
func DecadeNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractDecadeOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Millennium extracts millennium from a typed row value. Calendar and clock
// components require a date/local value; use LocalAt for an instant's zone.
//
// Example: expr.Millennium(input)
func Millennium[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MillenniumValue(AsSelected(rowInput(input))))
}

// MillenniumValue retains the selected aggregate/window phase.
//
// Example: expr.MillenniumValue(input)
func MillenniumValue[S any, V calendarValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractMillenniumOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// MillenniumNullable propagates NULL without changing its input scope.
//
// Example: expr.MillenniumNullable(input)
func MillenniumNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MillenniumNullableValue(AsSelected(rowInput(input))))
}

// MillenniumNullableValue is the nullable selected-value counterpart.
//
// Example: expr.MillenniumNullableValue(input)
func MillenniumNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractMillenniumOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// JulianDay returns the Julian day, including a fractional day for local
// date-times. Use LocalAt to select an instant's timezone first.
//
// Example: expr.JulianDay(input)
func JulianDay[S any, V calendarValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(JulianDayValue(AsSelected(rowInput(input))))
}

// JulianDayValue retains the selected aggregate/window phase.
//
// Example: expr.JulianDayValue(input)
func JulianDayValue[S any, V calendarValue](input Selected[S, V]) Selected[S, decimal.Decimal] {
	return operationValue[S](querycore.ExtractJulianDayOperation, columnCodec(codec.Decimal()), operationArg(input))
}

// JulianDayNullable propagates NULL without changing its input scope.
//
// Example: expr.JulianDayNullable(input)
func JulianDayNullable[S any, V calendarValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(JulianDayNullableValue(AsSelected(rowInput(input))))
}

// JulianDayNullableValue is the nullable selected-value counterpart.
//
// Example: expr.JulianDayNullableValue(input)
func JulianDayNullableValue[S any, V calendarValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](querycore.ExtractJulianDayOperation, columnCodec(codec.Nullable(codec.Decimal())), operationArg(input))
}

// Hour extracts the hour from a time or local date-time.
// Use LocalAt to select an instant's timezone first.
//
// Example: expr.Hour(input)
func Hour[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(HourValue(AsSelected(rowInput(input))))
}

// HourValue retains the selected aggregate/window phase.
//
// Example: expr.HourValue(input)
func HourValue[S any, V clockValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractHourOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// HourNullable propagates NULL without changing its input scope.
//
// Example: expr.HourNullable(input)
func HourNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(HourNullableValue(AsSelected(rowInput(input))))
}

// HourNullableValue is the nullable selected-value counterpart.
//
// Example: expr.HourNullableValue(input)
func HourNullableValue[S any, V clockValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractHourOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Minute extracts the minute from a time or local date-time.
// Use LocalAt to select an instant's timezone first.
//
// Example: expr.Minute(input)
func Minute[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MinuteValue(AsSelected(rowInput(input))))
}

// MinuteValue retains the selected aggregate/window phase.
//
// Example: expr.MinuteValue(input)
func MinuteValue[S any, V clockValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractMinuteOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// MinuteNullable propagates NULL without changing its input scope.
//
// Example: expr.MinuteNullable(input)
func MinuteNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MinuteNullableValue(AsSelected(rowInput(input))))
}

// MinuteNullableValue is the nullable selected-value counterpart.
//
// Example: expr.MinuteNullableValue(input)
func MinuteNullableValue[S any, V clockValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractMinuteOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// Second returns the exact seconds component, including fractional seconds,
// from a time or local date-time. Use LocalAt to select an instant's timezone first.
//
// Example: expr.Second(input)
func Second[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(SecondValue(AsSelected(rowInput(input))))
}

// SecondValue retains the selected aggregate/window phase.
//
// Example: expr.SecondValue(input)
func SecondValue[S any, V clockValue](input Selected[S, V]) Selected[S, decimal.Decimal] {
	return operationValue[S](querycore.ExtractSecondOperation, columnCodec(codec.Decimal()), operationArg(input))
}

// SecondNullable propagates NULL without changing its input scope.
//
// Example: expr.SecondNullable(input)
func SecondNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(SecondNullableValue(AsSelected(rowInput(input))))
}

// SecondNullableValue is the nullable selected-value counterpart.
//
// Example: expr.SecondNullableValue(input)
func SecondNullableValue[S any, V clockValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](querycore.ExtractSecondOperation, columnCodec(codec.Nullable(codec.Decimal())), operationArg(input))
}

// Milliseconds returns the seconds component multiplied by 1000, including
// fractional milliseconds. Use LocalAt to select an instant's timezone first.
//
// Example: expr.Milliseconds(input)
func Milliseconds[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(MillisecondsValue(AsSelected(rowInput(input))))
}

// MillisecondsValue retains the selected aggregate/window phase.
//
// Example: expr.MillisecondsValue(input)
func MillisecondsValue[S any, V clockValue](input Selected[S, V]) Selected[S, decimal.Decimal] {
	return operationValue[S](querycore.ExtractMillisecondsOperation, columnCodec(codec.Decimal()), operationArg(input))
}

// MillisecondsNullable propagates NULL without changing its input scope.
//
// Example: expr.MillisecondsNullable(input)
func MillisecondsNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(MillisecondsNullableValue(AsSelected(rowInput(input))))
}

// MillisecondsNullableValue is the nullable selected-value counterpart.
//
// Example: expr.MillisecondsNullableValue(input)
func MillisecondsNullableValue[S any, V clockValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](querycore.ExtractMillisecondsOperation, columnCodec(codec.Nullable(codec.Decimal())), operationArg(input))
}

// Microseconds returns the whole seconds component multiplied by 1000000 plus
// its fractional microseconds. Use LocalAt to select an instant's timezone first.
//
// Example: expr.Microseconds(input)
func Microseconds[S any, V clockValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(MicrosecondsValue(AsSelected(rowInput(input))))
}

// MicrosecondsValue retains the selected aggregate/window phase.
//
// Example: expr.MicrosecondsValue(input)
func MicrosecondsValue[S any, V clockValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractMicrosecondsOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// MicrosecondsNullable propagates NULL without changing its input scope.
//
// Example: expr.MicrosecondsNullable(input)
func MicrosecondsNullable[S any, V clockValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(MicrosecondsNullableValue(AsSelected(rowInput(input))))
}

// MicrosecondsNullableValue is the nullable selected-value counterpart.
//
// Example: expr.MicrosecondsNullableValue(input)
func MicrosecondsNullableValue[S any, V clockValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractMicrosecondsOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// UnixSeconds returns exact decimal seconds since the Unix epoch for an instant.
//
// Example: expr.UnixSeconds(input)
func UnixSeconds[S any, V instantValue](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(UnixSecondsValue(AsSelected(rowInput(input))))
}

// UnixSecondsValue retains the selected aggregate/window phase.
//
// Example: expr.UnixSecondsValue(input)
func UnixSecondsValue[S any, V instantValue](input Selected[S, V]) Selected[S, decimal.Decimal] {
	return operationValue[S](querycore.ExtractUnixSecondsOperation, columnCodec(codec.Decimal()), operationArg(input))
}

// UnixSecondsNullable propagates NULL without changing its input scope.
//
// Example: expr.UnixSecondsNullable(input)
func UnixSecondsNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(UnixSecondsNullableValue(AsSelected(rowInput(input))))
}

// UnixSecondsNullableValue is the nullable selected-value counterpart.
//
// Example: expr.UnixSecondsNullableValue(input)
func UnixSecondsNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](querycore.ExtractUnixSecondsOperation, columnCodec(codec.Nullable(codec.Decimal())), operationArg(input))
}

// UnixMilliseconds returns integer milliseconds since the Unix epoch.
// Fractional milliseconds round down, matching time.Time.UnixMilli before epoch.
//
// Example: expr.UnixMilliseconds(input)
func UnixMilliseconds[S any, V instantValue](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(UnixMillisecondsValue(AsSelected(rowInput(input))))
}

// UnixMillisecondsValue retains the selected aggregate/window phase.
//
// Example: expr.UnixMillisecondsValue(input)
func UnixMillisecondsValue[S any, V instantValue](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.ExtractUnixMillisecondsOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// UnixMillisecondsNullable propagates NULL without changing its input scope.
//
// Example: expr.UnixMillisecondsNullable(input)
func UnixMillisecondsNullable[S any, V instantValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(UnixMillisecondsNullableValue(AsSelected(rowInput(input))))
}

// UnixMillisecondsNullableValue is the nullable selected-value counterpart.
//
// Example: expr.UnixMillisecondsNullableValue(input)
func UnixMillisecondsNullableValue[S any, V instantValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.ExtractUnixMillisecondsOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}
