package temporal

import (
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Now reads an explicit application clock. It does not change the system clock
// or consult a process-global timezone. Callback failure returns no instant.
func Now(source clock.Clock) (DateTime, error) {
	if err := validateClock(source); err != nil {
		return DateTime{}, err
	}
	var result DateTime
	err := callback.Isolated("temporal clock", func() error { var err error; result, err = NewDateTime(source.Now()); return err })
	if err != nil {
		return DateTime{}, err
	}
	return result, nil
}

func validateClock(source clock.Clock) error {
	if source == nil {
		return fault.New(fault.Invalid, "application clock is required")
	}
	v := reflect.ValueOf(source)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		if v.IsNil() {
			return fault.New(fault.Invalid, "application clock is required")
		}
	}
	return nil
}

// Today uses calendar boundaries in the requested timezone, including DST.
func Today(source clock.Clock, zone *time.Location) (Date, error) {
	instant, err := Now(source)
	if err != nil {
		return Date{}, err
	}
	return instant.DateIn(zone)
}
func (v DateTime) DateIn(zone *time.Location) (Date, error) {
	local, err := v.LocalIn(zone)
	if err != nil {
		return Date{}, err
	}
	return local.Date(), nil
}

// FormatIn preserves the selected offset in an RFC3339Nano representation.
func (v DateTime) FormatIn(zone *time.Location) (string, error) {
	if _, err := v.LocalIn(zone); err != nil {
		return "", err
	}
	_, offset := v.instant.In(zone).Zone()
	if offset <= -24*3600 || offset >= 24*3600 || offset%60 != 0 {
		return "", fault.New(fault.Invalid, "timezone offset cannot be represented in RFC3339")
	}
	return v.instant.In(zone).Format(time.RFC3339Nano), nil
}

// ParseDateTimeIn accepts an explicit RFC3339 offset first, otherwise resolves a
// local wall time using the existing strict gap/overlap policy for zone.
func ParseDateTimeIn(text string, zone *time.Location) (DateTime, error) {
	if zone == nil {
		return DateTime{}, fault.New(fault.Invalid, "timezone is required")
	}
	if instant, err := ParseDateTime(text); err == nil {
		return instant, nil
	}
	local, err := ParseLocalDateTime(text)
	if err != nil {
		return DateTime{}, err
	}
	return local.In(zone)
}

func (v DateTime) UnixMilli() int64 { return v.instant.UnixMilli() }
func (v DateTime) UnixMicro() int64 { return v.instant.UnixMicro() }
