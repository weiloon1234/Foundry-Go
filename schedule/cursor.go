package schedule

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// progress is process-local per-schedule state that is not part of Snapshot.
// completed is the latest handled catch-up occurrence; deferred records when a
// due occurrence first waited for execution capacity.
type progress struct {
	completed time.Time
	deferred  time.Time
}

// CursorBackend is optional on the scheduler's lease backend. It persists, per
// schedule, the latest occurrence that completed or was deliberately skipped,
// so a restart or leadership change resumes catch-up after it instead of
// replaying completed occurrences. Advance must keep the maximum instant and
// may expire the entry after ttl. The Redis coordination client implements it;
// without it the cursor lives only in this scheduler process.
type CursorBackend interface {
	ScheduleCursor(context.Context, lease.Key) (time.Time, bool, error)
	AdvanceScheduleCursor(context.Context, lease.Key, time.Time, time.Duration) error
}

const cursorFamily lease.Name = "foundry.schedule.cursor"

func (s *Scheduler) cursorKey(id ID) (lease.Key, error) {
	return lease.NewKey(s.namespace, cursorFamily, string(s.config.Group)+":"+string(id))
}

// cursorTTL keeps a persisted cursor at least as long as the catch-up window
// can reach back, plus a day of slack for long outages.
func cursorTTL(window time.Duration) time.Duration { return window + 24*time.Hour }

// loadCursors reads persisted cursors for catch-up schedules. Failures fall back
// to the process-local cursor and are logged; they never block leadership.
func (s *Scheduler) loadCursors(ctx context.Context) map[int]time.Time {
	if s.cursors == nil {
		return nil
	}
	result := make(map[int]time.Time)
	for i, d := range s.registry.entries {
		if d.options.CatchUp.Window == 0 {
			continue
		}
		key, err := s.cursorKey(d.id)
		if err != nil {
			continue
		}
		operation, cancel := context.WithTimeout(ctx, s.operationTimeout())
		var at time.Time
		var found bool
		err = callback.Isolated("read schedule cursor", func() error {
			var err error
			at, found, err = s.cursors.ScheduleCursor(operation, key)
			return err
		})
		cancel()
		if err != nil {
			s.logFailure(ctx, d.id, "schedule cursor read failed", err)
			continue
		}
		if found && validInstant(at) {
			result[i] = at.UTC()
		}
	}
	return result
}

// advanceCursor records a handled occurrence locally and, when supported, in
// the coordination backend. It runs outside the scheduler mutex.
func (s *Scheduler) advanceCursor(index int, at time.Time) {
	d := s.registry.entries[index]
	if d.options.CatchUp.Window == 0 {
		return
	}
	s.mu.Lock()
	if at.After(s.progress[index].completed) {
		s.progress[index].completed = at
	}
	s.mu.Unlock()
	if s.cursors == nil {
		return
	}
	key, err := s.cursorKey(d.id)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.operationTimeout())
	defer cancel()
	if err := callback.Isolated("advance schedule cursor", func() error {
		return s.cursors.AdvanceScheduleCursor(ctx, key, at, cursorTTL(d.options.CatchUp.Window))
	}); err != nil {
		s.logFailure(ctx, d.id, "schedule cursor write failed", err)
	}
}

func (s *Scheduler) operationTimeout() time.Duration {
	return max(10*time.Millisecond, min(5*time.Second, s.config.LeadershipTTL/3))
}
