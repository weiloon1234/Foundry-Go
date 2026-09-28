package temporal

import (
	"regexp"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

var dateTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

// DateTime is an immutable UTC instant, normalized without a monotonic reading.
// Its zero value is Go's zero time (0001-01-01T00:00:00Z), not database null.
type DateTime struct{ instant time.Time }

func NewDateTime(instant time.Time) (DateTime, error) {
	instant = instant.UTC().Round(0)
	if instant.Year() < 1 || instant.Year() > 9999 {
		return DateTime{}, fault.New(fault.Invalid, "instant exceeds supported years")
	}
	return DateTime{instant}, nil
}
func ParseDateTime(text string) (DateTime, error) {
	if !dateTimePattern.MatchString(text) {
		return DateTime{}, fault.New(fault.Invalid, "expected RFC3339 date-time with an explicit offset")
	}
	v, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return DateTime{}, fault.New(fault.Invalid, "invalid date-time")
	}
	if v.Year() < 1 {
		return DateTime{}, fault.New(fault.Invalid, "date-time year is outside the supported range")
	}
	return NewDateTime(v)
}
func (v DateTime) UTC() time.Time { return v.instant }
func (v DateTime) IsZero() bool   { return v.instant.IsZero() }
func (v DateTime) String() string { return v.instant.Format(time.RFC3339Nano) }
func (v DateTime) Add(duration time.Duration) (DateTime, error) {
	return NewDateTime(v.instant.Add(duration))
}
func (v DateTime) LocalIn(zone *time.Location) (LocalDateTime, error) {
	if zone == nil {
		return LocalDateTime{}, fault.New(fault.Invalid, "timezone is required")
	}
	local := v.instant.In(zone)
	date, err := NewDate(local.Year(), local.Month(), local.Day())
	if err != nil {
		return LocalDateTime{}, err
	}
	clock, _ := NewTime(local.Hour(), local.Minute(), local.Second(), local.Nanosecond())
	return NewLocalDateTime(date, clock)
}
func (v DateTime) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *DateTime) UnmarshalText(data []byte) error {
	parsed, err := ParseDateTime(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
func (v *DateTime) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseDateTime)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
