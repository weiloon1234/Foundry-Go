package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"math"
)

type TemporalUnit uint8

const (
	UnitMicrosecond TemporalUnit = iota + 1
	UnitMillisecond
	UnitSecond
	UnitMinute
	UnitHour
	UnitDay
	UnitWeek
	UnitMonth
	UnitQuarter
	UnitYear
	UnitDecade
	UnitCentury
	UnitMillennium
)

func (u TemporalUnit) Name() string {
	if u < UnitMicrosecond || u > UnitMillennium {
		return ""
	}
	return [...]string{"", "microseconds", "milliseconds", "second", "minute", "hour", "day", "week", "month", "quarter", "year", "decade", "century", "millennium"}[u]
}

func TruncationUnitName(unit, minimum TemporalUnit) (string, error) {
	if unit < minimum || unit > UnitMillennium {
		return "", fault.New(fault.Invalid, "invalid temporal truncation unit")
	}
	return unit.Name(), nil
}
func ValidateGeographicPoint(latitude, longitude float64) error {
	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return fault.New(fault.Invalid, "invalid geographic coordinates")
	}
	return nil
}
func ValidateSubstringCount(count int32) error {
	if count < 0 {
		return fault.New(fault.Invalid, "substring count cannot be negative")
	}
	return nil
}
