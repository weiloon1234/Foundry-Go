package http

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Signing boundaries share clock ownership and supported timestamp validation.
func signingTime(applicationClock clock.Clock, operation string) (time.Time, error) {
	if nilCookieValue(applicationClock) {
		return time.Time{}, fault.New(fault.Invalid, "signing requires an application clock")
	}
	var now time.Time
	read := func() error { now = applicationClock.Now().UTC(); return nil }
	var err error
	if _, system := applicationClock.(clock.System); system {
		// The framework system clock cannot Goexit; avoid a goroutine per read.
		err = callback.Invoke(operation, read)
	} else {
		err = callback.Isolated(operation, read)
	}
	if err != nil {
		return time.Time{}, err
	}
	if now.Year() < 1970 || now.Year() > 9999 {
		return time.Time{}, fault.New(fault.Invalid, "signing clock is outside its supported range")
	}
	return now, nil
}
