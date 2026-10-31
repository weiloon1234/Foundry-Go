package temporal

import (
	"regexp"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

const timeLayout = "15:04:05.999999999"

var timePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?$`)

// Time is a wall-clock time at microsecond precision without a date or offset.
// Zero means midnight. Leap seconds and a 24:00 representation are rejected.
type Time struct{ nanos int64 }

// NewTime validates wall-clock fields and truncates fractional seconds to microsecond precision.
func NewTime(hour, minute, second, nanosecond int) (Time, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 || nanosecond < 0 || nanosecond >= 1e9 {
		return Time{}, safeerror.NewFault(fault.Invalid, "invalid wall-clock time")
	}
	nanosecond = int(time.Duration(nanosecond).Truncate(time.Microsecond))
	return Time{int64(hour)*int64(time.Hour) + int64(minute)*int64(time.Minute) + int64(second)*int64(time.Second) + int64(nanosecond)}, nil
}

// ParseTime parses HH:MM:SS with optional fractional seconds and normalizes through NewTime.
func ParseTime(text string) (Time, error) {
	if !timePattern.MatchString(text) {
		return Time{}, safeerror.NewFault(fault.Invalid, "expected HH:MM:SS with optional nanoseconds")
	}
	v, err := time.Parse(timeLayout, text)
	if err != nil {
		return Time{}, safeerror.NewFault(fault.Invalid, "invalid wall-clock time")
	}
	return NewTime(v.Hour(), v.Minute(), v.Second(), v.Nanosecond())
}

// Hour returns the hour of the day.
func (v Time) Hour() int { return int(v.nanos / int64(time.Hour)) }

// Minute returns the minute within the hour.
func (v Time) Minute() int { return int(v.nanos / int64(time.Minute) % 60) }

// Second returns the second within the minute.
func (v Time) Second() int { return int(v.nanos / int64(time.Second) % 60) }

// Nanosecond returns the fractional second in nanoseconds.
func (v Time) Nanosecond() int { return int(v.nanos % int64(time.Second)) }

// String returns the wall-clock time with fractional seconds as needed.
func (v Time) String() string {
	return time.Date(1, 1, 1, v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC).Format(timeLayout)
}

// MarshalText encodes the wall-clock time as text.
func (v Time) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

// UnmarshalText parses wall-clock text, replacing the receiver only on success.
func (v *Time) UnmarshalText(data []byte) error {
	parsed, err := ParseTime(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// UnmarshalJSON decodes a JSON wall-clock time, replacing the receiver only on success.
func (v *Time) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseTime)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
