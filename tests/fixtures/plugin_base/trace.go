package pluginbase

import (
	"context"
	"slices"
	"sync"
)

// Trace is a test-owned domain service shared by independent plugins. Its
// notification channel avoids sleeping/polling while testing actual worker work.
type Trace struct {
	mu      sync.Mutex
	entries []string
	changed chan struct{}
}

func NewTrace() *Trace { return &Trace{changed: make(chan struct{})} }
func (t *Trace) Record(entry string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, entry)
	close(t.changed)
	t.changed = make(chan struct{})
}
func (t *Trace) Entries() []string { t.mu.Lock(); defer t.mu.Unlock(); return slices.Clone(t.entries) }
func (t *Trace) Wait(ctx context.Context, entry string) error {
	for {
		t.mu.Lock()
		found := slices.Contains(t.entries, entry)
		changed := t.changed
		t.mu.Unlock()
		if found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
