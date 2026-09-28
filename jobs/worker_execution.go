package jobs

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func idFromExecution[P any](id ExecutionID) ID[P] {
	return model.IDFromBytes[ExecutionOf[P]](id.Bytes())
}
func (w *Worker) process(parent context.Context, key Key, reservation Reservation) (processErr error) {
	envelope := reservation.Envelope
	if err := ValidateEnqueue(context.Background(), key, envelope); err != nil {
		return err
	}
	if err := reservation.Ownership.Validate(); err != nil {
		return err
	}
	if reservation.Ownership.ID() != envelope.ID() {
		return fault.New(fault.Internal, "backend returned mismatched job ownership")
	}
	parent, processErr = attribution.WithContext(parent, envelope.Origin())
	if processErr != nil {
		return processErr
	}
	parent = tracing.WithoutContext(parent)
	if trace, present := envelope.Trace().Get(); present {
		parent, processErr = tracing.WithContext(parent, trace)
		if processErr != nil {
			return processErr
		}
	}
	observed, span, _ := observability.FromContext(parent).Start(parent, observability.Operation{Kind: observability.Job, Name: observability.Name(envelope.Name())})
	if observed != nil {
		parent = observed
	}
	outcome := observability.Panicked
	defer func() {
		if processErr != nil {
			outcome = observability.OutcomeFor(processErr)
		}
		span.End(observability.Result{Outcome: outcome})
	}()
	if parent.Err() != nil {
		outcome = observability.Cancelled
		return w.finish(key, reservation.Ownership, Result{State: Waiting, Reason: WorkerStopped})
	}
	entry, err := w.registry.lookup(jobKey{envelope.Name(), envelope.Version()})
	if err != nil || entry.prepare == nil {
		outcome = observability.Failed
		return w.finish(key, reservation.Ownership, Result{State: Failed, Reason: Unregistered})
	}
	work, cancel := context.WithCancelCause(parent)
	defer cancel(context.Canceled)
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go func() { heartbeatDone <- w.heartbeat(key, reservation.Ownership, cancel, stopHeartbeat) }()
	// Finalization begins only after all preparation, handler and middleware
	// callbacks exit. Heartbeats therefore also cover context-ignoring admission.
	finish := func(result Result) error {
		outcome = jobOutcome(result)
		close(stopHeartbeat)
		if err := <-heartbeatDone; err != nil {
			return err
		}
		return w.finish(key, reservation.Ownership, result)
	}
	invocation, stopTimeout := context.WithTimeout(work, envelope.Policy().Timeout)
	defer stopTimeout()
	frame := &executionFrame{worker: w, typ: entry.typ}
	frame.active.Store(true)
	prepareContext := context.WithValue(invocation, executionKey{}, frame)
	var invoke func(context.Context) error
	var delay time.Duration
	var invalidPayload bool
	prepareErr := callback.Isolated("prepare job", func() error {
		var err error
		invoke, delay, err = entry.prepare(prepareContext, envelope.PayloadJSON())
		invalidPayload = errorgraph.Has[*payloadError](err)
		return err
	})
	frame.active.Store(false)
	if invalidPayload {
		return finish(Result{State: Failed, Reason: PayloadInvalid})
	}
	if delay > 0 && prepareErr == nil && invocation.Err() == nil {
		return finish(Result{State: Waiting, Delay: delay, Reason: RateLimited})
	}
	if parent.Err() != nil || errors.Is(context.Cause(invocation), ErrCancelled) || errors.Is(context.Cause(invocation), ErrOwnershipLost) {
		return finish(Result{State: Waiting, Reason: WorkerStopped})
	}
	operation, stopOperation := context.WithTimeout(work, w.config.OperationTimeout)
	var attempt uint32
	var cancelRequested bool
	startErr := callback.Isolated("start job", func() error {
		var err error
		attempt, err = w.backend.JobStart(operation, key, reservation.Ownership)
		cancelRequested = errorgraph.Is(err, ErrCancelled)
		return err
	})
	stopOperation()
	if startErr != nil {
		if cancelRequested || parent.Err() != nil {
			return finish(Result{State: Waiting, Reason: WorkerStopped})
		}
		close(stopHeartbeat)
		heartbeatErr := <-heartbeatDone
		return errors.Join(startErr, heartbeatErr)
	}
	if attempt == 0 || attempt > envelope.Policy().Attempts || attempt != reservation.Attempts+1 {
		close(stopHeartbeat)
		return errors.Join(fault.New(fault.Internal, "backend returned an invalid job attempt"), <-heartbeatDone)
	}
	frame = &executionFrame{worker: w, typ: entry.typ, attempt: Attempt{ID: envelope.ID(), Name: envelope.Name(), Version: envelope.Version(), Queue: envelope.Queue(), Number: attempt}}
	frame.active.Store(true)
	handlerContext := context.WithValue(invocation, executionKey{}, frame)
	handlerErr := prepareErr
	if handlerErr == nil {
		if invoke == nil {
			handlerErr = fault.New(fault.Internal, "job preparation returned no handler")
		} else {
			handlerErr = callback.Isolated("execute job", func() error { return invoke(handlerContext) })
		}
	}
	cause := context.Cause(invocation)
	result := safeExecutionResult(envelope.Policy(), attempt, handlerErr, cause, parent.Err() != nil)
	if frame.noRetry.Load() && result.State == Waiting && result.Reason != CancelRequested {
		result.State, result.Delay = Failed, 0
	}
	frame.active.Store(false)
	return finish(result)
}

func (w *Worker) heartbeat(key Key, proof Ownership, cancel context.CancelCauseFunc, stop <-chan struct{}) error {
	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			ctx, done := context.WithTimeout(context.Background(), w.config.OperationTimeout)
			var status LeaseStatus
			err := callback.Isolated("renew job reservation", func() error {
				var err error
				status, err = w.backend.JobRenew(ctx, key, proof, w.config.LeaseDuration)
				return err
			})
			done()
			if err != nil {
				cancel(ErrOwnershipLost)
				return err
			}
			if !status.Owned {
				cancel(ErrOwnershipLost)
				return ErrOwnershipLost
			}
			if status.CancellationRequested {
				cancel(ErrCancelled)
			}
		}
	}
}
func (w *Worker) finish(key Key, proof Ownership, result Result) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.config.OperationTimeout)
	defer cancel()
	var owned bool
	err := callback.Isolated("finish job", func() error {
		var err error
		owned, err = w.backend.JobFinish(ctx, key, proof, result)
		return err
	})
	if err != nil {
		return err
	}
	if !owned {
		return ErrOwnershipLost
	}
	return nil
}

// Error Is/As/Unwrap are extension callbacks too. Keep the execution frame and
// heartbeat alive until classification actually returns, including Goexit.
func safeExecutionResult(policy Policy, attempt uint32, err, cause error, stopping bool) Result {
	var result Result
	failed := callback.Isolated("classify job outcome", func() error {
		result = executionResult(policy, attempt, err, cause, stopping)
		return nil
	})
	if failed != nil {
		return executionResult(policy, attempt, fault.New(fault.Panicked, "job outcome classifier failed"), nil, stopping)
	}
	return result
}
func executionResult(policy Policy, attempt uint32, err, cause error, stopping bool) Result {
	reason := NoReason
	switch {
	case errors.Is(cause, ErrCancelled):
		reason = CancelRequested
	case errorgraph.Has[*permanentError](err):
		return Result{State: Failed, Reason: HandlerFailed}
	case errors.Is(cause, context.DeadlineExceeded):
		reason = TimedOut
	case stopping:
		reason = WorkerStopped
	case errorgraph.Has[*payloadError](err):
		return Result{State: Failed, Reason: PayloadInvalid}
	case errorgraph.Is(err, fault.Panicked):
		reason = HandlerPanicked
	case err != nil:
		reason = HandlerFailed
	}
	if reason == NoReason {
		return Result{State: Succeeded}
	}
	if attempt >= policy.Attempts {
		return Result{State: Failed, Reason: reason}
	}
	delay, _ := policy.RetryDelay(attempt)
	if policy.Jitter > 0 {
		delay += rand.N(policy.Jitter + 1)
	}
	return Result{State: Waiting, Reason: reason, Delay: delay}
}
