package temporal

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

// LocalDateTime preserves a calendar date and wall time without assuming any
// timezone. Zero is absent. Converting to an instant requires an explicit zone.
type LocalDateTime struct {
	date  Date
	clock Time
}

func NewLocalDateTime(date Date, clock Time) (LocalDateTime, error) {
	if date.IsZero() {
		return LocalDateTime{}, fault.New(fault.Invalid, "local date-time requires a date")
	}
	return LocalDateTime{date, clock}, nil
}
func ParseLocalDateTime(text string) (LocalDateTime, error) {
	if len(text) < 19 || (text[10] != 'T' && text[10] != ' ') {
		return LocalDateTime{}, fault.New(fault.Invalid, "expected local YYYY-MM-DDTHH:MM:SS")
	}
	date, err := ParseDate(text[:10])
	if err != nil {
		return LocalDateTime{}, err
	}
	clock, err := ParseTime(text[11:])
	if err != nil {
		return LocalDateTime{}, err
	}
	return NewLocalDateTime(date, clock)
}
func (v LocalDateTime) Date() Date     { return v.date }
func (v LocalDateTime) Time() Time     { return v.clock }
func (v LocalDateTime) IsZero() bool   { return v.date.IsZero() }
func (v LocalDateTime) String() string { return v.date.String() + "T" + v.clock.String() }
func (v LocalDateTime) Add(duration time.Duration) (LocalDateTime, error) {
	if v.IsZero() {
		return LocalDateTime{}, fault.New(fault.Invalid, "local date-time is absent")
	}
	wall := v.wall().Add(duration)
	date, err := NewDate(wall.Year(), wall.Month(), wall.Day())
	if err != nil {
		return LocalDateTime{}, err
	}
	clock, _ := NewTime(wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond())
	return NewLocalDateTime(date, clock)
}
func (v LocalDateTime) wall() time.Time { return v.date.wall().Add(time.Duration(v.clock.nanos)) }

// In resolves a unique instant. DST gaps fail with Invalid; repeated wall times
// fail with Conflict. Supply an explicit offset to ParseDateTime to disambiguate.
// Zone offsets outside +/-24 hours are rejected; standard IANA zones fit within
// this bound. No machine-local timezone or arbitrary DST choice is used.
func (v LocalDateTime) In(zone *time.Location) (DateTime, error) {
	if zone == nil || v.IsZero() {
		return DateTime{}, fault.New(fault.Invalid, "local date-time and timezone are required")
	}
	wall := v.wall()
	start, end := wall.Add(-24*time.Hour), wall.Add(24*time.Hour)
	var match time.Time
	matched := false
	seen := make(map[int]bool)
	for cursor, steps := start, 0; !cursor.After(end); steps++ {
		if steps >= 256 {
			return DateTime{}, fault.New(fault.Invalid, "timezone has excessive transitions")
		}
		local := cursor.In(zone)
		_, offset := local.Zone()
		if offset < -24*3600 || offset > 24*3600 {
			return DateTime{}, fault.New(fault.Invalid, "timezone offset exceeds supported bounds")
		}
		if !seen[offset] {
			seen[offset] = true
			candidate := wall.Add(-time.Duration(offset) * time.Second)
			actual := candidate.In(zone)
			if actual.Year() == wall.Year() && actual.Month() == wall.Month() && actual.Day() == wall.Day() && actual.Hour() == wall.Hour() && actual.Minute() == wall.Minute() && actual.Second() == wall.Second() && actual.Nanosecond() == wall.Nanosecond() {
				if matched && !match.Equal(candidate) {
					return DateTime{}, fault.New(fault.Conflict, "local date-time is ambiguous in this timezone")
				}
				match = candidate
				matched = true
			}
		}
		_, next := local.ZoneBounds()
		if next.IsZero() || next.After(end) {
			break
		}
		if !next.After(cursor) {
			return DateTime{}, fault.New(fault.Invalid, "invalid timezone transition")
		}
		cursor = next
	}
	if !matched {
		return DateTime{}, fault.New(fault.Invalid, "local date-time does not exist in this timezone")
	}
	return NewDateTime(match)
}
func (v LocalDateTime) MarshalText() ([]byte, error) {
	if v.IsZero() {
		return nil, fault.New(fault.Invalid, "local date-time is absent")
	}
	return []byte(v.String()), nil
}
func (v *LocalDateTime) UnmarshalText(data []byte) error {
	parsed, err := ParseLocalDateTime(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
func (v *LocalDateTime) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseLocalDateTime)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
