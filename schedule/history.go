package schedule

import (
	"slices"
	"time"
)

type State string

const (
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
	Skipped   State = "skipped"
)

type Reason string

const (
	NoReason           Reason = ""
	HandlerFailed      Reason = "handler_failed"
	HookFailed         Reason = "hook_failed"
	Panicked           Reason = "panicked"
	TimedOut           Reason = "timed_out"
	Stopped            Reason = "stopped"
	LeadershipLost     Reason = "leadership_lost"
	OverlapLost        Reason = "overlap_lost"
	OverlapBusy        Reason = "overlap_busy"
	CoordinationFailed Reason = "coordination_failed"
	CapacityReached    Reason = "capacity_reached"
	Missed             Reason = "missed"
	BacklogLimited     Reason = "backlog_limited"
	ClockFailed        Reason = "clock_failed"
)

// Record is bounded process-local operational history, not a durable ledger.
// It deliberately contains classifications instead of arbitrary callback errors.
type Record struct {
	Execution  ExecutionID
	Invocation Invocation
	State      State
	Reason     Reason
	StartedAt  time.Time
	FinishedAt time.Time
}
type ScheduleStatus struct {
	ID      ID
	Enabled bool
	Next    time.Time
	Active  int
}
type Snapshot struct {
	Running                bool
	Stopping               bool
	Leader                 bool
	Active                 int
	LeadershipAcquisitions uint64
	LeadershipLosses       uint64
	CoordinationFailures   uint64
	Schedules              []ScheduleStatus
	History                []Record
}

// Snapshot copies status/history under one lock and can be used from callbacks.
// Authorize any operational endpoint exposing attribution or occurrence IDs.
func (s *Scheduler) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{Running: s.running, Stopping: s.stopped, Leader: s.leader, Active: s.active,
		LeadershipAcquisitions: s.acquisitions, LeadershipLosses: s.losses, CoordinationFailures: s.coordinationFailures,
		Schedules: slices.Clone(s.status), History: slices.Clone(s.history)}
}
func (s *Scheduler) record(record Record) {
	if len(s.history) == s.config.MaxHistory {
		copy(s.history, s.history[1:])
		s.history[len(s.history)-1] = record
	} else {
		s.history = append(s.history, record)
	}
}
