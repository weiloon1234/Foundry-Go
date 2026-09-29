package schedule

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Window limits occurrences to local wall-clock times in [From, To) in the
// spec's zone (UTC for intervals). From after To wraps midnight, for example
// 22:00 to 06:00. Unless inverts the window: occurrences inside it are skipped.
// The zero Window allows every time.
type Window struct {
	From, To temporal.Time
	Unless   bool
}

// Between returns a window admitting local times in [from, to).
func Between(from, to temporal.Time) Window { return Window{From: from, To: to} }

// UnlessBetween returns a window skipping local times in [from, to).
func UnlessBetween(from, to temporal.Time) Window { return Window{From: from, To: to, Unless: true} }

func (w Window) enabled() bool { return w != (Window{}) }
func (w Window) Validate() error {
	if w.enabled() && offset(w.From) == offset(w.To) {
		return fault.New(fault.Invalid, "schedule window requires distinct start and end times")
	}
	return nil
}
func offset(t temporal.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
}
func (w Window) admits(local time.Time) bool {
	if !w.enabled() {
		return true
	}
	at := time.Duration(local.Hour())*time.Hour + time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
	from, to := offset(w.From), offset(w.To)
	inside := from <= at && at < to
	if from > to {
		inside = at >= from || at < to
	}
	return inside != w.Unless
}

// Weekdays returns Monday through Friday for Options.Days.
func Weekdays() []time.Weekday {
	return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
}

// Weekends returns Saturday and Sunday for Options.Days.
func Weekends() []time.Weekday { return []time.Weekday{time.Saturday, time.Sunday} }

// Predicate decides at execution time whether an occurrence runs. It is an
// owned callback: it receives the invocation context and must honor it.
type Predicate func(context.Context, Invocation) (bool, error)

func validateDays(days []time.Weekday) error {
	if len(days) > 7 {
		return fault.New(fault.Invalid, "schedule days exceed one week")
	}
	for i, day := range days {
		if day < time.Sunday || day > time.Saturday || slices.Contains(days[:i], day) {
			return fault.New(fault.Invalid, "invalid or duplicate schedule day")
		}
	}
	return nil
}

// admits applies the calendar filters to one occurrence in the spec's zone.
// They need no callback, so the scheduler evaluates them at admission; a
// filtered occurrence behaves like a time the spec never produced.
func (o Options) admits(spec Spec, at time.Time) bool {
	if len(o.Days) == 0 && !o.LastDayOfMonth && !o.Between.enabled() {
		return true
	}
	local := at.In(spec.location())
	if len(o.Days) > 0 && !slices.Contains(o.Days, local.Weekday()) {
		return false
	}
	if o.LastDayOfMonth && local.AddDate(0, 0, 1).Month() == local.Month() {
		return false
	}
	return o.Between.admits(local)
}
