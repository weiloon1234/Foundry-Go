package jobs

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// QueueConfig bounds retained work. Backends reject capacity overflow rather
// than evicting unfinished jobs. Retention begins at terminal completion and
// must cover the application's publication/recovery window.
type QueueConfig struct {
	MaxEntries int
	MaxBytes   int64
	MaxHistory int
	Retention  time.Duration
}

func DefaultQueueConfig() QueueConfig {
	return QueueConfig{MaxEntries: 4096, MaxBytes: 64 << 20, MaxHistory: 64, Retention: 7 * 24 * time.Hour}
}

func (c QueueConfig) Validate() error {
	if c.MaxEntries <= 0 || c.MaxEntries > 1<<20 || c.MaxBytes <= 0 || c.MaxBytes > 1<<40 || c.MaxHistory <= 0 || c.MaxHistory > 4096 || c.Retention < time.Millisecond || c.Retention > MaxDelay || c.Retention%time.Millisecond != 0 {
		return fault.New(fault.Invalid, "invalid job queue capacity, history or retention")
	}
	return nil
}
