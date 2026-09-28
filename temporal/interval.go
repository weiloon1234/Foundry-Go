package temporal

import (
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

// Interval keeps calendar months/days separate from elapsed time. A calendar
// day need not be 24 hours when applied to an instant in a timezone. Zero is a
// zero interval. Values are immutable and retain signed components.
type Interval struct {
	months, days int32
	elapsed      time.Duration
}

// NewInterval constructs calendar and elapsed components without normalizing
// them into one another. Sub-microsecond precision is rejected.
func NewInterval(months, days int32, elapsed time.Duration) (Interval, error) {
	if elapsed%time.Microsecond != 0 {
		return Interval{}, fault.New(fault.Invalid, "interval requires whole microseconds")
	}
	return Interval{months: months, days: days, elapsed: elapsed}, nil
}

// Months constructs a signed calendar-month interval.
func Months(n int32) Interval { return Interval{months: n} }

// Days constructs a signed calendar-day interval, not an elapsed-hour duration.
func Days(n int32) Interval { return Interval{days: n} }

// Elapsed constructs an elapsed-time interval with whole microseconds.
func Elapsed(d time.Duration) (Interval, error) { return NewInterval(0, 0, d) }

func (v Interval) Months() int32          { return v.months }
func (v Interval) Days() int32            { return v.days }
func (v Interval) Elapsed() time.Duration { return v.elapsed }

// String retains every signed component in an unambiguous interval spelling.
func (v Interval) String() string {
	return fmt.Sprintf("%+d mons %+d days %+d microseconds", v.months, v.days, v.elapsed/time.Microsecond)
}

func (v Interval) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *Interval) UnmarshalText(data []byte) error {
	parsed, err := ParseInterval(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
func (v *Interval) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseInterval)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
