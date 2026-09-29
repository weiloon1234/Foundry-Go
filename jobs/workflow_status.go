package jobs

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// WorkflowStatus is safe progress metadata for one workflow: counts over its
// steps and completion job (callbacks excluded), never payloads. Pending counts
// unfinished jobs (waiting, blocked, reserved, running); Processed counts
// terminal ones. Failed reports that a member failed or was cancelled.
// FinishedAt is set once every job, callbacks included, is terminal.
type WorkflowStatus struct {
	ID         WorkflowID   `json:"id"`
	Kind       WorkflowKind `json:"kind"`
	Total      int          `json:"total"`
	Pending    int          `json:"pending"`
	Processed  int          `json:"processed"`
	Succeeded  int          `json:"succeeded"`
	FailedJobs int          `json:"failed_jobs"`
	Cancelled  int          `json:"cancelled"`
	Failed     bool         `json:"failed"`
	Cancelling bool         `json:"cancelling"`
	FinishedAt time.Time    `json:"finished_at,omitzero"`
}

// Count adds one member's state; backends use it to build a status.
func (s *WorkflowStatus) Count(state State) {
	s.Total++
	switch state {
	case Succeeded:
		s.Processed++
		s.Succeeded++
	case Failed:
		s.Processed++
		s.FailedJobs++
	case Cancelled:
		s.Processed++
		s.Cancelled++
	default:
		s.Pending++
	}
}

// Finished reports whether every job, callbacks included, is terminal.
func (s WorkflowStatus) Finished() bool { return !s.FinishedAt.IsZero() }

// Progress is the processed fraction in [0, 1].
func (s WorkflowStatus) Progress() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Processed) / float64(s.Total)
}

// WorkflowStatusBackend is optional; the memory and Redis authorities
// implement it. A retired workflow (finished beyond retention) is absent.
type WorkflowStatusBackend interface {
	JobWorkflowStatus(context.Context, Key, WorkflowID) (value.Optional[WorkflowStatus], error)
}

// WorkflowStatus inspects one workflow's progress in queue.
func (d *Dispatcher) WorkflowStatus(ctx context.Context, queue Queue, id WorkflowID) (value.Optional[WorkflowStatus], error) {
	release, err := d.begin(ctx)
	if err != nil {
		return value.Optional[WorkflowStatus]{}, err
	}
	defer release()
	if id.IsZero() {
		return value.Optional[WorkflowStatus]{}, fault.New(fault.Invalid, "workflow status requires an identity")
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return value.Optional[WorkflowStatus]{}, err
	}
	backend, ok := d.backend.(WorkflowStatusBackend)
	if !ok {
		return value.Optional[WorkflowStatus]{}, fault.New(fault.Invalid, "job backend does not report workflow status")
	}
	return backend.JobWorkflowStatus(ctx, key, id)
}

// Status inspects this workflow's progress through dispatcher.
func (w Workflow) Status(ctx context.Context, dispatcher *Dispatcher) (value.Optional[WorkflowStatus], error) {
	if dispatcher == nil {
		return value.Optional[WorkflowStatus]{}, fault.New(fault.Invalid, "workflow status requires a dispatcher")
	}
	return dispatcher.WorkflowStatus(ctx, w.envelope.Queue(), w.ID())
}
