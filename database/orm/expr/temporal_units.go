package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type calendarValue interface {
	temporal.Date | temporal.LocalDateTime
}

type clockValue interface {
	temporal.Time | temporal.LocalDateTime
}

type instantValue interface{ time.Time | temporal.DateTime }

type temporalUnit = querycore.TemporalUnit

const (
	unitMicrosecond = querycore.UnitMicrosecond
	unitMillisecond = querycore.UnitMillisecond
	unitSecond      = querycore.UnitSecond
	unitMinute      = querycore.UnitMinute
	unitHour        = querycore.UnitHour
	unitDay         = querycore.UnitDay
	unitWeek        = querycore.UnitWeek
	unitMonth       = querycore.UnitMonth
	unitQuarter     = querycore.UnitQuarter
	unitYear        = querycore.UnitYear
	unitDecade      = querycore.UnitDecade
	unitCentury     = querycore.UnitCentury
	unitMillennium  = querycore.UnitMillennium
)

// DateUnit identifies a calendar boundary. Clock units cannot be supplied to
// TruncateDate without an explicit invalid conversion.
//
// Example: expr.DateMonth
type DateUnit uint8

const (
	DateDay        DateUnit = DateUnit(unitDay)
	DateWeek       DateUnit = DateUnit(unitWeek)
	DateMonth      DateUnit = DateUnit(unitMonth)
	DateQuarter    DateUnit = DateUnit(unitQuarter)
	DateYear       DateUnit = DateUnit(unitYear)
	DateDecade     DateUnit = DateUnit(unitDecade)
	DateCentury    DateUnit = DateUnit(unitCentury)
	DateMillennium DateUnit = DateUnit(unitMillennium)
)

// TimestampUnit identifies a timestamp boundary, including fractional seconds.
//
// Example: expr.TimestampDay
type TimestampUnit uint8

const (
	TimestampMicrosecond TimestampUnit = TimestampUnit(unitMicrosecond)
	TimestampMillisecond TimestampUnit = TimestampUnit(unitMillisecond)
	TimestampSecond      TimestampUnit = TimestampUnit(unitSecond)
	TimestampMinute      TimestampUnit = TimestampUnit(unitMinute)
	TimestampHour        TimestampUnit = TimestampUnit(unitHour)
	TimestampDay         TimestampUnit = TimestampUnit(unitDay)
	TimestampWeek        TimestampUnit = TimestampUnit(unitWeek)
	TimestampMonth       TimestampUnit = TimestampUnit(unitMonth)
	TimestampQuarter     TimestampUnit = TimestampUnit(unitQuarter)
	TimestampYear        TimestampUnit = TimestampUnit(unitYear)
	TimestampDecade      TimestampUnit = TimestampUnit(unitDecade)
	TimestampCentury     TimestampUnit = TimestampUnit(unitCentury)
	TimestampMillennium  TimestampUnit = TimestampUnit(unitMillennium)
)

// TimeZone names an explicitly selected IANA timezone; its zero value is invalid.
//
// Example: expr.UTCZone()
type TimeZone struct{ name string }

// LoadTimeZone validates an explicit IANA timezone.
//
// Example: expr.LoadTimeZone(name)
func LoadTimeZone(name string) (TimeZone, error) {
	zone, err := querycore.LoadTimeZone(name)
	return TimeZone{name: zone}, err
}

// UTCZone selects UTC without a session default.
//
// Example: expr.UTCZone()
func UTCZone() TimeZone { return TimeZone{name: "UTC"} }

// String returns the explicitly selected IANA timezone name.
func (z TimeZone) String() string { return z.name }

// LocalResolution explicitly selects PostgreSQL's DST gap/overlap policy.
//
// Example: expr.PostgresStandardTime
type LocalResolution uint8

const PostgresStandardTime LocalResolution = LocalResolution(querycore.PostgresStandardTime)

func textOperationArgument(text string, err error) ormcore.OperationArgument {
	v, bindErr := codec.String[string]().Bind(text)
	return scalarParameter(codec.TypeText, v, safeerror.Join("operation failed", err, bindErr))
}

func unitArgument(unit, minimum temporalUnit) ormcore.OperationArgument {
	name, err := querycore.TruncationUnitName(unit, minimum)
	return textOperationArgument(name, err)
}

func zoneArgument(zone TimeZone) ormcore.OperationArgument {
	name, err := querycore.ZoneArgument(zone.name)
	return textOperationArgument(name, err)
}
func resolutionArgument(zone TimeZone, policy LocalResolution) ormcore.OperationArgument {
	name, err := querycore.ResolutionArgument(zone.name, uint8(policy))
	return textOperationArgument(name, err)
}

func intervalArgument(interval temporal.Interval) ormcore.OperationArgument {
	v, err := codec.Interval().Bind(interval)
	return scalarParameter(codec.TypeInterval, v, err)
}

func elapsedArgument(duration time.Duration) ormcore.OperationArgument {
	interval, err := temporal.Elapsed(duration)
	arg := intervalArgument(interval)
	if err != nil {
		arg.Value.Err = safeerror.Join("operation failed", arg.Value.Err, err)
	}
	return arg
}
