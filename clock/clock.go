// Package clock supplies injectable application time. Lifecycle safety deadlines
// use real context deadlines and are intentionally independent of frozen clocks.
package clock

import "time"

// Clock returns the current application time. Consumers pass it explicitly to
// operations needing deterministic time rather than replacing process globals.
type Clock interface{ Now() time.Time }

// System reads the operating system clock. Application time has database
// (microsecond) precision.
type System struct{}

// Now returns the current operating system time in UTC, truncated to
// microsecond precision.
func (System) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
