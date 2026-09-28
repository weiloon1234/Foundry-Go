package codec

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Interval preserves signed months, days and elapsed microseconds independently.
// It accepts PostgreSQL output styles without relying on session IntervalStyle.
// Elapsed components must fit time.Duration; unsupported values never normalize
// elapsed hours into calendar days merely to fit that bound.
func Interval() Codec[temporal.Interval] {
	return typed(TypeInterval, func(v temporal.Interval) (driver.Value, error) { return v.String(), nil }, func(source any) (temporal.Interval, error) {
		v, err := text(source)
		if err != nil {
			return temporal.Interval{}, err
		}
		return temporal.ParseInterval(v)
	})
}
