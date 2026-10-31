package schedule

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Calendar constructs ordinary declarations in one configured timezone. It owns
// no worker or registry; execution reuses the existing scheduler and DST policy.
type Calendar struct{ zone *time.Location }

// NewCalendar resolves an explicit timezone for calendar declarations.
func NewCalendar(name temporal.ZoneName) (Calendar, error) {
	zone, err := name.Location()
	return Calendar{zone: zone}, err
}

// In selects an explicit zone without changing this calendar or its declarations.
func (c Calendar) In(name temporal.ZoneName) (Calendar, error) { return NewCalendar(name) }

// Cron declares a cron expression in the calendar's timezone.
func (c Calendar) Cron(id ID, expression string, handler Handler) (Declaration, error) {
	return Cron(id, expression, c.zone, handler)
}

// DailyAt declares a daily HH:MM occurrence in the calendar's timezone.
func (c Calendar) DailyAt(id ID, text string, handler Handler) (Declaration, error) {
	return DailyAt(id, text, c.zone, handler)
}

// Hourly declares an occurrence at the start of each local hour.
func (c Calendar) Hourly(id ID, handler Handler) (Declaration, error) {
	return Hourly(id, c.zone, handler)
}

// Daily declares an occurrence at local midnight.
func (c Calendar) Daily(id ID, handler Handler) (Declaration, error) {
	return Daily(id, c.zone, handler)
}

// Weekly declares an occurrence at local midnight each Monday.
func (c Calendar) Weekly(id ID, handler Handler) (Declaration, error) {
	return Weekly(id, c.zone, handler)
}

// Monthly declares an occurrence at local midnight on the first of each month.
func (c Calendar) Monthly(id ID, handler Handler) (Declaration, error) {
	return Monthly(id, c.zone, handler)
}

// EveryMinute declares an occurrence at each local minute boundary.
func (c Calendar) EveryMinute(id ID, handler Handler) (Declaration, error) {
	return EveryMinute(id, c.zone, handler)
}

// EveryFiveMinutes declares occurrences at local minutes divisible by five.
func (c Calendar) EveryFiveMinutes(id ID, handler Handler) (Declaration, error) {
	return EveryFiveMinutes(id, c.zone, handler)
}

// EveryTenMinutes declares occurrences at local minutes divisible by ten.
func (c Calendar) EveryTenMinutes(id ID, handler Handler) (Declaration, error) {
	return EveryTenMinutes(id, c.zone, handler)
}

// EveryFifteenMinutes declares occurrences at local minutes divisible by fifteen.
func (c Calendar) EveryFifteenMinutes(id ID, handler Handler) (Declaration, error) {
	return EveryFifteenMinutes(id, c.zone, handler)
}

// EveryThirtyMinutes declares occurrences at local minutes zero and thirty.
func (c Calendar) EveryThirtyMinutes(id ID, handler Handler) (Declaration, error) {
	return EveryThirtyMinutes(id, c.zone, handler)
}

// LastDayOfMonthAt declares an HH:MM occurrence on the last local day of each month.
func (c Calendar) LastDayOfMonthAt(id ID, text string, handler Handler) (Declaration, error) {
	return LastDayOfMonthAt(id, text, c.zone, handler)
}
