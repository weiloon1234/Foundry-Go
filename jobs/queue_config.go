package jobs

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// QueueConfig bounds retained work. MaxEntries and MaxBytes cap live
// (unfinished) jobs: a full queue rejects enqueue with fault.Overloaded, which
// is retryable, and never evicts unfinished work. Terminal records are kept for
// deduplication and inspection until Retention after completion, bounded
// separately by MaxRetained records and MaxBytes of their own; beyond either
// bound the oldest terminal independent records (then finished workflows) are
// evicted first. Enqueue never fails because terminal records are retained.
// Zero MaxRetained retains up to MaxEntries terminal records. Retention and
// MaxRetained must cover the application's publication/recovery window.
type QueueConfig struct {
	MaxEntries  int
	MaxBytes    int64
	MaxHistory  int
	Retention   time.Duration
	MaxRetained int
}

func DefaultQueueConfig() QueueConfig {
	return QueueConfig{MaxEntries: 4096, MaxBytes: 64 << 20, MaxHistory: 64, Retention: 7 * 24 * time.Hour, MaxRetained: 65536}
}

func (c QueueConfig) Validate() error {
	if c.MaxEntries <= 0 || c.MaxEntries > 1<<20 || c.MaxBytes <= 0 || c.MaxBytes > 1<<40 || c.MaxHistory <= 0 || c.MaxHistory > 4096 || c.Retention < time.Millisecond || c.Retention > MaxDelay || c.Retention%time.Millisecond != 0 || c.MaxRetained < 0 || c.MaxRetained > 1<<24 {
		return fault.New(fault.Invalid, "invalid job queue capacity, history or retention")
	}
	return nil
}

// RetainedLimit is the effective terminal-record bound.
func (c QueueConfig) RetainedLimit() int {
	if c.MaxRetained == 0 {
		return c.MaxEntries
	}
	return c.MaxRetained
}

// ErrQueueFull reports that live work reached the queue's capacity. Nothing
// was enqueued; retry after workers drain the queue.
var ErrQueueFull = fault.New(fault.Overloaded, "job queue capacity is exhausted")
