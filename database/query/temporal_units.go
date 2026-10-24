package query

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
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
type TimeZone struct{ name string }

// LoadTimeZone validates an explicit IANA timezone. Example: LoadTimeZone("Asia/Kuala_Lumpur").
func LoadTimeZone(name string) (TimeZone, error) {
	zone, err := querycore.LoadTimeZone(name)
	return TimeZone{name: zone}, err
}

// UTCZone selects UTC without a session default. Example: UTCZone().
func UTCZone() TimeZone           { return TimeZone{name: "UTC"} }
func (z TimeZone) String() string { return z.name }

// LocalResolution explicitly selects PostgreSQL's DST gap/overlap policy.
type LocalResolution uint8

const PostgresStandardTime LocalResolution = LocalResolution(querycore.PostgresStandardTime)

func textOperationArgument[S any](text string, err error) operationArgument {
	e := parameterExpression[S](text, codec.String[string]()).Value()
	if err != nil {
		e.node = parameterNode{Err: err}
	}
	return operationArg(e)
}

func unitArgument[S any](unit, minimum temporalUnit) operationArgument {
	name, err := querycore.TruncationUnitName(unit, minimum)
	return textOperationArgument[S](name, err)
}

func zoneArgument[S any](zone TimeZone) operationArgument {
	name, err := querycore.ZoneArgument(zone.name)
	return textOperationArgument[S](name, err)
}
func resolutionArgument[S any](zone TimeZone, policy LocalResolution) operationArgument {
	name, err := querycore.ResolutionArgument(zone.name, uint8(policy))
	return textOperationArgument[S](name, err)
}

func intervalArgument[S any](interval temporal.Interval) operationArgument {
	return operationArg(parameterExpression[S](interval, codec.Interval()).Value())
}

func elapsedArgument[S any](duration time.Duration) operationArgument {
	interval, err := temporal.Elapsed(duration)
	arg := intervalArgument[S](interval)
	if err != nil {
		arg.Value = parameterNode{Kind: codec.TypeInterval, Err: err}
	}
	return arg
}
