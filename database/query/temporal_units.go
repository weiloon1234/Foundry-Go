package query

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type calendarValue interface {
	temporal.Date | temporal.LocalDateTime
}
type clockValue interface {
	temporal.Time | temporal.LocalDateTime
}
type instantValue interface{ time.Time | temporal.DateTime }

type temporalUnit uint8

const (
	unitMicrosecond temporalUnit = iota + 1
	unitMillisecond
	unitSecond
	unitMinute
	unitHour
	unitDay
	unitWeek
	unitMonth
	unitQuarter
	unitYear
	unitDecade
	unitCentury
	unitMillennium
)

func (u temporalUnit) name() string {
	if u < unitMicrosecond || u > unitMillennium {
		return ""
	}
	return [...]string{"", "microseconds", "milliseconds", "second", "minute", "hour", "day", "week", "month", "quarter", "year", "decade", "century", "millennium"}[u]
}

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

// TimeZone names an explicitly selected IANA timezone. The zero value is invalid.
// PostgreSQL resolves the name using its timezone database when executing SQL.
type TimeZone struct{ name string }

// LoadTimeZone validates a named zone using Go's timezone database. Local and
// empty names are rejected to avoid process-specific timezone selection.
func LoadTimeZone(name string) (TimeZone, error) {
	if _, err := temporal.LoadTimeZone(name); err != nil {
		return TimeZone{}, fault.Wrap(fault.Invalid, "invalid query timezone", err)
	}
	return TimeZone{name: name}, nil
}

// UTCZone selects UTC without loading an application or session default.
func UTCZone() TimeZone           { return TimeZone{name: "UTC"} }
func (z TimeZone) String() string { return z.name }

// LocalResolution makes PostgreSQL's choice at DST gaps/overlaps explicit.
// This differs from temporal.LocalDateTime.In, which requires a unique instant.
type LocalResolution uint8

const PostgresStandardTime LocalResolution = 1

func textOperationArgument[S any](text string, err error) operationArgument {
	e := parameterExpression[S](text, codec.String[string]()).Value()
	if err != nil {
		e.node = parameterNode{err: err}
	}
	return operationArg(e)
}
func unitArgument[S any](unit, minimum temporalUnit) operationArgument {
	var err error
	if unit < minimum || unit > unitMillennium {
		err = fault.New(fault.Invalid, "invalid temporal truncation unit")
	}
	return textOperationArgument[S](unit.name(), err)
}
func zoneArgument[S any](zone TimeZone) operationArgument {
	var err error
	if zone.name == "" {
		err = fault.New(fault.Invalid, "temporal operation requires a timezone")
	}
	return textOperationArgument[S](zone.name, err)
}
func resolutionArgument[S any](zone TimeZone, policy LocalResolution) operationArgument {
	if policy != PostgresStandardTime {
		return textOperationArgument[S]("", fault.New(fault.Invalid, "local resolution requires an explicit supported DST policy"))
	}
	return zoneArgument[S](zone)
}

func intervalArgument[S any](interval temporal.Interval) operationArgument {
	return operationArg(parameterExpression[S](interval, codec.Interval()).Value())
}
func elapsedArgument[S any](duration time.Duration) operationArgument {
	interval, err := temporal.Elapsed(duration)
	arg := intervalArgument[S](interval)
	if err != nil {
		arg.value = parameterNode{kind: codec.TypeInterval, err: err}
	}
	return arg
}
