// Package temporal separates UTC instants, calendar dates, wall times, and local
// date-times. Value operations return new values; decoding replaces the receiver
// only on success. Application time comes from the clock package.
package temporal

import (
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

// Date is a calendar date without a time or timezone. The zero value is absent
// and cannot be serialized. Supported years are 1 through 9999.
type Date struct {
	year  uint16
	month uint8
	day   uint8
}

func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return Date{}, fault.New(fault.Invalid, "invalid calendar date")
	}
	v := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if v.Day() != day {
		return Date{}, fault.New(fault.Invalid, "invalid calendar date")
	}
	return Date{uint16(year), uint8(month), uint8(day)}, nil
}
func ParseDate(text string) (Date, error) {
	if len(text) != 10 {
		return Date{}, fault.New(fault.Invalid, "expected YYYY-MM-DD")
	}
	v, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return Date{}, fault.New(fault.Invalid, "invalid calendar date")
	}
	return NewDate(v.Year(), v.Month(), v.Day())
}
func (d Date) Year() int         { return int(d.year) }
func (d Date) Month() time.Month { return time.Month(d.month) }
func (d Date) Day() int          { return int(d.day) }
func (d Date) IsZero() bool      { return d.year == 0 }
func (d Date) String() string    { return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day) }
func (d Date) AddDays(days int) (Date, error) {
	if d.IsZero() {
		return Date{}, fault.New(fault.Invalid, "date is absent")
	}
	// A valid result can never be more than the entire supported calendar away.
	if days < -3652058 || days > 3652058 {
		return Date{}, fault.New(fault.Invalid, "date arithmetic exceeds supported years")
	}
	v := d.wall().AddDate(0, 0, days)
	return NewDate(v.Year(), v.Month(), v.Day())
}
func (d Date) wall() time.Time { return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC) }
func (d Date) MarshalText() ([]byte, error) {
	if d.IsZero() {
		return nil, fault.New(fault.Invalid, "date is absent")
	}
	return []byte(d.String()), nil
}
func (d *Date) UnmarshalText(data []byte) error {
	v, err := ParseDate(string(data))
	if err != nil {
		return err
	}
	*d = v
	return nil
}
func (d *Date) UnmarshalJSON(data []byte) error {
	v, err := textvalue.Decode(data, ParseDate)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
