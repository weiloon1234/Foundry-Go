package temporal

import (
	"regexp"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

var dateTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

// DateTime is an immutable UTC instant at microsecond precision, normalized
// without a monotonic reading. Its zero value is Go's zero time
// (0001-01-01T00:00:00Z), not database null.
type DateTime struct{ instant time.Time }

// NewDateTime converts an instant to UTC at microsecond precision, stripping monotonic time and
// checking supported years.
func NewDateTime(instant time.Time) (DateTime, error) {
	// Truncate strips sub-microsecond precision and also strips the process-local
	// monotonic reading; Round(0) is retained to keep monotonic stripping explicit.
	instant = instant.UTC().Round(0).Truncate(time.Microsecond)
	if instant.Year() < 1 || instant.Year() > 9999 {
		return DateTime{}, safeerror.NewFault(fault.Invalid, "instant exceeds supported years")
	}
	return DateTime{instant}, nil
}

// ParseDateTime parses an RFC3339 date-time with an explicit offset and normalizes it through
// NewDateTime.
func ParseDateTime(text string) (DateTime, error) {
	if !dateTimePattern.MatchString(text) {
		return DateTime{}, safeerror.NewFault(fault.Invalid, "expected RFC3339 date-time with an explicit offset")
	}
	v, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return DateTime{}, safeerror.NewFault(fault.Invalid, "invalid date-time")
	}
	if v.Year() < 1 {
		return DateTime{}, safeerror.NewFault(fault.Invalid, "date-time year is outside the supported range")
	}
	return NewDateTime(v)
}

// UTC returns the underlying UTC instant.
func (v DateTime) UTC() time.Time { return v.instant }

// IsZero reports whether the underlying instant is the zero time.
func (v DateTime) IsZero() bool { return v.instant.IsZero() }

// String returns the UTC instant in RFC3339 format with fractional seconds as needed.
func (v DateTime) String() string { return v.instant.Format(time.RFC3339Nano) }

// Add adds a duration and validates the resulting instant.
func (v DateTime) Add(duration time.Duration) (DateTime, error) {
	return NewDateTime(v.instant.Add(duration))
}

// LocalIn returns local wall-clock fields in the supplied nonnil time zone.
func (v DateTime) LocalIn(zone *time.Location) (LocalDateTime, error) {
	if zone == nil {
		return LocalDateTime{}, safeerror.NewFault(fault.Invalid, "timezone is required")
	}
	local := v.instant.In(zone)
	date, err := NewDate(local.Year(), local.Month(), local.Day())
	if err != nil {
		return LocalDateTime{}, err
	}
	clock, _ := NewTime(local.Hour(), local.Minute(), local.Second(), local.Nanosecond())
	return NewLocalDateTime(date, clock)
}

// MarshalText encodes the instant's RFC3339 string as text.
func (v DateTime) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

// UnmarshalText parses date-time text, replacing the receiver only on success.
func (v *DateTime) UnmarshalText(data []byte) error {
	parsed, err := ParseDateTime(string(data))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// UnmarshalJSON decodes a JSON date-time, replacing the receiver only on success.
func (v *DateTime) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseDateTime)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
