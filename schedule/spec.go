// Package schedule provides bounded, coordinated scheduled execution. Calendar
// parsing is independent of runtime ownership and always uses an explicit zone.
package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const MaxInterval = 365 * 24 * time.Hour

// Spec is an immutable parsed cron or anchored interval. Calendar schedules use
// local wall time: nonexistent DST times are skipped, repeated times each denote
// a separate UTC occurrence. Intervals measure elapsed time and ignore DST.
type Spec struct {
	source   string
	calendar *cron.SpecSchedule
	interval time.Duration
	anchor   time.Time
}

// ParseCron accepts five fields (minute first) or six (second first). No year,
// embedded timezone or @ directives are accepted. Day-of-month/day-of-week use
// standard cron OR semantics when both are restricted. Use temporal.ParseTimeZone
// to resolve configuration strings before calling this constructor.
func ParseCron(source string, zone *time.Location) (Spec, error) {
	if zone == nil || zone == time.Local || zone.String() == "" || zone.String() == "Local" || len(source) > 512 {
		return Spec{}, fault.New(fault.Invalid, "cron requires a bounded expression and explicit timezone")
	}
	fields := strings.Fields(source)
	if len(fields) < 5 || len(fields) > 6 {
		return Spec{}, fault.New(fault.Invalid, "cron requires five or six fields")
	}
	for _, field := range fields {
		if strings.ContainsAny(field, "=@") || strings.HasPrefix(field, ",") || strings.HasSuffix(field, ",") || strings.Contains(field, ",,") {
			return Spec{}, fault.New(fault.Invalid, "invalid cron field")
		}
	}
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	parsed, err := parser.Parse(strings.Join(fields, " "))
	if err != nil {
		return Spec{}, fault.New(fault.Invalid, "invalid cron expression")
	}
	calendar, ok := parsed.(*cron.SpecSchedule)
	if !ok {
		return Spec{}, fault.New(fault.Internal, "unexpected cron parser result")
	}
	ownedZone := *zone
	calendar.Location = &ownedZone
	// Reject structurally valid but unreachable dates such as February 31.
	if calendar.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return Spec{}, fault.New(fault.Invalid, "cron has no reachable occurrence")
	}
	return Spec{source: strings.Join(fields, " "), calendar: calendar}, nil
}

// Interval aligns whole-millisecond occurrences to the UTC Unix epoch. Alignment
// survives process restarts; use IntervalFrom for an explicit phase/first instant.
func Interval(every time.Duration) (Spec, error) { return IntervalFrom(every, time.Unix(0, 0)) }
func IntervalFrom(every time.Duration, anchor time.Time) (Spec, error) {
	if every < time.Millisecond || every > MaxInterval || every%time.Millisecond != 0 || !validInstant(anchor) || anchor.Nanosecond()%int(time.Millisecond) != 0 {
		return Spec{}, fault.New(fault.Invalid, "interval requires whole milliseconds, a bounded period and valid anchor")
	}
	return Spec{interval: every, anchor: anchor.UTC(), source: every.String()}, nil
}
func validInstant(at time.Time) bool { year := at.UTC().Year(); return year >= 1970 && year <= 9999 }
func (s Spec) Validate() error {
	if s.calendar == nil && s.interval == 0 {
		return fault.New(fault.Invalid, "schedule specification is not initialized")
	}
	return nil
}
func (s Spec) String() string { return s.source }
func (s Spec) TimeZone() string {
	if s.calendar != nil {
		return s.calendar.Location.String()
	}
	if s.interval != 0 {
		return "UTC"
	}
	return ""
}

// Next returns a strictly later occurrence. Missing means the calendar has no
// future occurrence within its bounded search/range; it never loops indefinitely.
func (s Spec) Next(after time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	if !validInstant(after) {
		return time.Time{}, fault.New(fault.Invalid, "invalid schedule cursor")
	}
	var next time.Time
	if s.calendar != nil {
		next = s.calendar.Next(after).UTC()
	} else if after.Before(s.anchor) {
		next = s.anchor
	} else {
		period := s.interval.Milliseconds()
		elapsed := after.UnixMilli() - s.anchor.UnixMilli()
		next = time.UnixMilli(s.anchor.UnixMilli() + (elapsed/period+1)*period).UTC()
	}
	if next.IsZero() || !validInstant(next) || !next.After(after) {
		return time.Time{}, fault.New(fault.Missing, "schedule has no future occurrence")
	}
	return next, nil
}

func dailySpec(text string, zone *time.Location) (Spec, error) {
	if len(text) != 5 {
		return Spec{}, fault.New(fault.Invalid, "daily time must be HH:MM")
	}
	wall, err := temporal.ParseTime(text + ":00")
	if err != nil {
		return Spec{}, err
	}
	return ParseCron(fmt.Sprintf("0 %d %d * * *", wall.Minute(), wall.Hour()), zone)
}
