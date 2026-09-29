package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// FailedJob is one terminal failure as the worker finalized it: the complete
// envelope (payload included, so an operator can re-dispatch it), its queue
// and classification. Exceptions includes the final attempt when it was one.
type FailedJob struct {
	Queue      Queue
	Envelope   Envelope
	Reason     Reason
	Attempts   uint32
	Exceptions uint32
	Retries    uint32
	FailedAt   time.Time
}

func (FailedJob) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("failed job")) }

// FailureSink receives terminal failures after the queue confirmed them, for
// example a durable archive (jobs/archive). It runs as an owned callback bounded
// by the worker's OperationTimeout; its error is logged and never changes the
// job's outcome. Recording is at most once per confirmed failure: a crash
// between finalization and recording can miss an entry.
type FailureSink interface {
	RecordFailure(context.Context, FailedJob) error
}

// WithFailureSink attaches a sink to a standalone worker.
func WithFailureSink(sink FailureSink) WorkerOption {
	return func(worker *Worker) error {
		if sink == nil || isNil(sink) {
			return fault.New(fault.Invalid, "job failure sink is required")
		}
		worker.sinks = append(worker.sinks, sink)
		return nil
	}
}

type failureSinkContribution struct{ sink FailureSink }

func failureSinks(key foundation.Key[*Dispatcher]) foundation.Collection[failureSinkContribution] {
	return foundation.NewCollection[failureSinkContribution](fmt.Sprintf("jobs.failure-sinks.%q", key.Name()))
}

// RegisterFailureSink contributes a sink to every worker kernel consuming the
// dispatcher named by key. id names the sink uniquely for that dispatcher.
func RegisterFailureSink(r *foundation.Registrar, key foundation.Key[*Dispatcher], id string, construct func(foundation.Resolver) (FailureSink, error)) error {
	if construct == nil || r == nil || key.Name() == "" {
		return fault.New(fault.Invalid, "job failure sink requires a dispatcher and constructor")
	}
	return foundation.Contribute(r, failureSinks(key), id, func(resolver foundation.Resolver) (failureSinkContribution, error) {
		sink, err := construct(resolver)
		if err != nil {
			return failureSinkContribution{}, err
		}
		if sink == nil || isNil(sink) {
			return failureSinkContribution{}, fault.New(fault.Invalid, "job failure sink constructor returned nil")
		}
		return failureSinkContribution{sink}, nil
	})
}

// recordFailure hands one confirmed terminal failure to every sink.
func (w *Worker) recordFailure(ctx context.Context, key Key, failed FailedJob) {
	for _, sink := range w.sinks {
		operation, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.config.OperationTimeout)
		err := callback.Isolated("record job failure", func() error { return sink.RecordFailure(operation, failed) })
		cancel()
		if err != nil {
			w.logBackendFailure(ctx, "archive", key, err)
		}
	}
}

// Redispatch submits a copy of envelope as a new job: a fresh execution ID,
// immediate availability, no uniqueness window and no retry deadline, keeping
// its payload, remaining policy and attribution. It is the explicit operator path for archived failures; the
// name/version must be registered with this dispatcher.
func (d *Dispatcher) Redispatch(ctx context.Context, envelope Envelope) (ExecutionID, error) {
	id, err := model.NewID[Execution]()
	if err != nil {
		return ExecutionID{}, err
	}
	copied := envelope
	copied.wire.ID = id
	copied.wire.AvailableAt = time.Time{}
	copied.wire.Unique = Uniqueness{}
	copied.wire.Policy = envelope.wire.Policy.snapshot()
	// An operator re-dispatch starts a fresh retry budget and deadline.
	copied.wire.Policy.RetryUntil = time.Time{}
	copied.wire.EnvelopeVersion = envelope.wire.EnvelopeVersion
	if copied.wire.EnvelopeVersion == ExtendedEnvelope && !copied.wire.extended() {
		copied.wire.EnvelopeVersion = 0
		if copied.wire.Trace.IsSet() {
			copied.wire.EnvelopeVersion = TracedEnvelope
		}
	}
	key, inserted, err := func() (Key, bool, error) {
		release, err := d.begin(ctx)
		if err != nil {
			return Key{}, false, err
		}
		defer release()
		if err := copied.Validate(); err != nil {
			return Key{}, false, err
		}
		if _, err := d.registry.lookup(jobKey{copied.Name(), copied.Version()}); err != nil {
			return Key{}, false, err
		}
		key, err := NewKey(d.config.Namespace, copied.Queue())
		if err != nil {
			return Key{}, false, err
		}
		inserted, err := d.backend.JobEnqueue(ctx, key, copied)
		return key, inserted, err
	}()
	if err != nil {
		return ExecutionID{}, err
	}
	if inserted {
		d.runInline(ctx, key)
	}
	return id, nil
}
