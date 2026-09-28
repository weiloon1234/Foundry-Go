// Package clock supplies injectable application time. Lifecycle safety deadlines
// use real context deadlines and are intentionally independent of frozen clocks.
package clock

import "time"

// Clock returns the current application time. Consumers pass it explicitly to
// operations needing deterministic time rather than replacing process globals.
type Clock interface{ Now() time.Time }

// System reads the operating system clock.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }
