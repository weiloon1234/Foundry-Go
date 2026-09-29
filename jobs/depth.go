package jobs

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// DefaultDepthInterval is the sampling period of a DepthMonitor.
const DefaultDepthInterval = 15 * time.Second

// DepthTarget selects one queue of one connection to sample.
type DepthTarget struct {
	Connection ConnectionName
	Queue      Queue
	Dispatcher *Dispatcher
}

// QueueDepth is one timestamped reading. Available is false until the first
// successful sample, or when the last sample failed (Stats then keeps the
// previous reading).
type QueueDepth struct {
	Connection ConnectionName
	Queue      Queue
	Stats      QueueStats
	SampledAt  time.Time
	Available  bool
}

// DepthMonitor samples queue statistics in the background (Dispatcher.Stats)
// and serves the latest readings without I/O, for metrics collectors that
// must not block a scrape on a queue authority.
type DepthMonitor struct {
	targets  []DepthTarget
	interval time.Duration
	mu       sync.Mutex
	readings []QueueDepth
}

func NewDepthMonitor(interval time.Duration, targets ...DepthTarget) (*DepthMonitor, error) {
	if interval == 0 {
		interval = DefaultDepthInterval
	}
	if interval < time.Second || interval > time.Hour || len(targets) == 0 || len(targets) > 256 {
		return nil, fault.New(fault.Invalid, "queue depth monitor requires 1 to 256 targets and a 1s to 1h interval")
	}
	monitor := &DepthMonitor{targets: slices.Clone(targets), interval: interval, readings: make([]QueueDepth, len(targets))}
	for i, target := range targets {
		if target.Dispatcher == nil || target.Queue.Validate() != nil {
			return nil, fault.New(fault.Invalid, "queue depth target requires a dispatcher and queue")
		}
		monitor.readings[i] = QueueDepth{Connection: target.Connection, Queue: target.Queue}
	}
	return monitor, nil
}

// Run samples every interval until ctx ends. A backend without statistics or
// a failed sample marks that reading unavailable; sampling continues.
func (m *DepthMonitor) Run(ctx context.Context) error {
	if m == nil || ctx == nil {
		return fault.New(fault.Invalid, "queue depth monitor requires initialization and context")
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		m.Sample(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Sample reads every target once now.
func (m *DepthMonitor) Sample(ctx context.Context) {
	for i, target := range m.targets {
		operation, cancel := context.WithTimeout(ctx, min(m.interval, 5*time.Second))
		stats, err := target.Dispatcher.Stats(operation, target.Queue)
		cancel()
		m.mu.Lock()
		if err == nil {
			m.readings[i].Stats, m.readings[i].SampledAt, m.readings[i].Available = stats, time.Now().UTC(), true
		} else {
			m.readings[i].Available = false
		}
		m.mu.Unlock()
	}
}

// Snapshot returns the latest readings without I/O.
func (m *DepthMonitor) Snapshot() []QueueDepth {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.readings)
}
