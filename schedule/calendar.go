package schedule

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Calendar constructs ordinary declarations in one configured timezone. It owns
// no worker or registry; execution reuses the existing scheduler and DST policy.
type Calendar struct{ zone *time.Location }

func NewCalendar(name temporal.ZoneName) (Calendar, error) {
	zone, err := name.Location()
	return Calendar{zone: zone}, err
}

// In selects an explicit zone without changing this calendar or its declarations.
func (c Calendar) In(name temporal.ZoneName) (Calendar, error) { return NewCalendar(name) }
func (c Calendar) Cron(id ID, expression string, handler Handler) (Declaration, error) {
	return Cron(id, expression, c.zone, handler)
}
func (c Calendar) DailyAt(id ID, text string, handler Handler) (Declaration, error) {
	return DailyAt(id, text, c.zone, handler)
}
func (c Calendar) Hourly(id ID, handler Handler) (Declaration, error) {
	return Hourly(id, c.zone, handler)
}
func (c Calendar) Daily(id ID, handler Handler) (Declaration, error) {
	return Daily(id, c.zone, handler)
}
func (c Calendar) Weekly(id ID, handler Handler) (Declaration, error) {
	return Weekly(id, c.zone, handler)
}
func (c Calendar) Monthly(id ID, handler Handler) (Declaration, error) {
	return Monthly(id, c.zone, handler)
}
