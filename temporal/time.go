package temporal

import (
	"regexp"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

const timeLayout = "15:04:05.999999999"

var timePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?$`)

// Time is a wall-clock time without a date or offset. Zero means midnight.
// Leap seconds and a 24:00 representation are rejected.
type Time struct{ nanos int64 }

func NewTime(hour, minute, second, nanosecond int) (Time, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 || nanosecond < 0 || nanosecond >= 1e9 {
		return Time{}, fault.New(fault.Invalid, "invalid wall-clock time")
	}
	return Time{int64(hour)*int64(time.Hour) + int64(minute)*int64(time.Minute) + int64(second)*int64(time.Second) + int64(nanosecond)}, nil
}
func ParseTime(text string) (Time, error) {
	if !timePattern.MatchString(text) {
		return Time{}, fault.New(fault.Invalid, "expected HH:MM:SS with optional nanoseconds")
	}
	v, err := time.Parse(timeLayout, text)
	if err != nil {
		return Time{}, fault.New(fault.Invalid, "invalid wall-clock time")
	}
	return NewTime(v.Hour(), v.Minute(), v.Second(), v.Nanosecond())
}
func (v Time) Hour() int       { return int(v.nanos / int64(time.Hour)) }
func (v Time) Minute() int     { return int(v.nanos / int64(time.Minute) % 60) }
func (v Time) Second() int     { return int(v.nanos / int64(time.Second) % 60) }
func (v Time) Nanosecond() int { return int(v.nanos % int64(time.Second)) }
func (v Time) String() string {
	return time.Date(1, 1, 1, v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC).Format(timeLayout)
}
func (v Time) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *Time) UnmarshalText(data []byte) error {
	parsed, err := ParseTime(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
func (v *Time) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseTime)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
