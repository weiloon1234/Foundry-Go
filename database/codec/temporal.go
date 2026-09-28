package codec

import (
	"database/sql/driver"
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func validInstant(v time.Time) bool {
	return v.Year() >= 1 && v.Year() <= 9999 && v.Nanosecond()%1000 == 0
}

// Time maps time.Time to an instant (timestamptz), normalizing UTC and discarding
// its process-local monotonic reading. Precision below a microsecond is rejected
// rather than silently truncated. Supported years match Foundry temporal types.
func Time() Codec[time.Time] {
	return typed(TypeDateTime, func(v time.Time) (driver.Value, error) {
		v = v.UTC().Round(0)
		if !validInstant(v) {
			return nil, invalid()
		}
		return v, nil
	}, func(source any) (time.Time, error) {
		v, ok := source.(time.Time)
		if !ok || !validInstant(v.UTC()) {
			return time.Time{}, invalid()
		}
		return v.UTC().Round(0), nil
	})
}

// DateTime maps Foundry's UTC instant to PostgreSQL timestamptz.
func DateTime() Codec[temporal.DateTime] {
	base := Time()
	return typed(base.ParameterType(), func(v temporal.DateTime) (driver.Value, error) { return base.Bind(v.UTC()) }, func(source any) (temporal.DateTime, error) {
		v, err := base.Decode(source)
		if err != nil {
			return temporal.DateTime{}, err
		}
		return temporal.NewDateTime(v)
	})
}

// Date maps a calendar date without inferring a timezone or truncating a time.
func Date() Codec[temporal.Date] {
	return typed(TypeDate, func(v temporal.Date) (driver.Value, error) {
		if v.IsZero() {
			return nil, invalid()
		}
		return v.String(), nil
	}, func(source any) (temporal.Date, error) {
		if v, ok := source.(time.Time); ok {
			if v.Hour() != 0 || v.Minute() != 0 || v.Second() != 0 || v.Nanosecond() != 0 {
				return temporal.Date{}, invalid()
			}
			return temporal.NewDate(v.Year(), v.Month(), v.Day())
		}
		v, err := text(source)
		if err != nil {
			return temporal.Date{}, err
		}
		return temporal.ParseDate(v)
	})
}

// WallTime maps a time-of-day to PostgreSQL time without time zone. Offset time,
// 24:00, leap seconds and sub-microsecond values are outside this contract.
func WallTime() Codec[temporal.Time] {
	return typed(TypeTime, func(v temporal.Time) (driver.Value, error) {
		if v.Nanosecond()%1000 != 0 {
			return nil, invalid()
		}
		return v.String(), nil
	}, func(source any) (temporal.Time, error) {
		v, err := text(source)
		if err != nil {
			return temporal.Time{}, err
		}
		parsed, err := temporal.ParseTime(v)
		if err != nil || parsed.Nanosecond()%1000 != 0 {
			return temporal.Time{}, invalid()
		}
		return parsed, nil
	})
}

// LocalDateTime maps wall date-time components to timestamp without time zone.
// Driver time.Time locations are not converted: this value has no instant/zone.
func LocalDateTime() Codec[temporal.LocalDateTime] {
	return typed(TypeLocalDateTime, func(v temporal.LocalDateTime) (driver.Value, error) {
		if v.IsZero() || v.Time().Nanosecond()%1000 != 0 {
			return nil, invalid()
		}
		return v.String(), nil
	}, func(source any) (temporal.LocalDateTime, error) {
		var raw string
		if v, ok := source.(time.Time); ok {
			if !validInstant(v) {
				return temporal.LocalDateTime{}, invalid()
			}
			raw = v.Format("2006-01-02T15:04:05.999999999")
		} else {
			var err error
			raw, err = text(source)
			if err != nil {
				return temporal.LocalDateTime{}, err
			}
		}
		parsed, err := temporal.ParseLocalDateTime(raw)
		if err != nil || parsed.Time().Nanosecond()%1000 != 0 {
			return temporal.LocalDateTime{}, invalid()
		}
		return parsed, nil
	})
}
