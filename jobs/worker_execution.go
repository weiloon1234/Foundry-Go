package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func idFromExecution[P any](id ExecutionID) ID[P] {
	return model.IDFromBytes[ExecutionOf[P]](id.Bytes())
}

// attemptState collects what one reservation produced so logging and
// observation happen once, after finalization, from the same facts.
type attemptState struct {
	number     uint32
	completion Result
	finalized  bool
	requested  bool
	failure    error
}

// process owns one reservation until finalization. It returns false when a
// backend or adapter failure occurred, so the calling loop backs off before
// reserving again. It never stops the worker.
func (w *Worker) process(run workerRun, key Key, reservation Reservation, reservedAt time.Time) bool {
	envelope := reservation.Envelope
	if err := ValidateEnqueue(context.Background(), key, envelope); err != nil {
		w.logBackendFailure(run.work, "reserve", key, err)
		return false
	}
	if err := reservation.Ownership.Validate(); err != nil {
		w.logBackendFailure(run.work, "reserve", key, err)
		return false
	}
	if reservation.Ownership.ID() != envelope.ID() {
		w.logBackendFailure(run.work, "reserve", key, fault.New(fault.Internal, "backend returned mismatched job ownership"))
		return false
	}
	parent := tracing.WithoutContext(run.work)
	parent, originErr := attribution.WithContext(parent, envelope.Origin())
	if originErr == nil {
		if trace, present := envelope.Trace().Get(); present {
			parent, originErr = tracing.WithContext(parent, trace)
		}
	}
	if originErr != nil {
		parent = run.work
	}
	observed, span, _ := observability.FromContext(parent).Start(parent, observability.Operation{Kind: observability.Job, Name: observability.Name(envelope.Name())})
	if observed != nil {
		parent = observed
	}
	state := attemptState{number: reservation.Attempts}
	outcome := observability.Panicked
	defer func() {
		// Describe only when a reporter or failure log can receive it; error
		// inspection runs extension methods, so skip it when nothing listens.
		var diagnostic fault.Diagnostic
		if state.failure != nil && (span != nil || state.requested && w.logger != nil && w.config.FailureLog) {
			diagnostic = errordiag.Describe(state.failure)
		}
		span.EndWithDiagnostic(observability.Result{Outcome: outcome}, diagnostic)
		if state.requested {
			w.logAttempt(parent, envelope, state.number, reservation.Retries, state.completion, state.finalized, diagnostic)
		}
		if state.requested && state.finalized && state.completion.State == Failed && len(w.sinks) > 0 {
			exceptions := reservation.Exceptions
			if state.completion.Reason.exception() {
				exceptions++
			}
			w.recordFailure(parent, key, FailedJob{Queue: key.Queue(), Envelope: envelope, Reason: state.completion.Reason, Attempts: state.number, Exceptions: exceptions, Retries: reservation.Retries, FailedAt: time.Now().UTC()})
		}
	}()
	validUntil := reservedAt.Add(w.config.LeaseDuration)
	// finish records one requested transition. Unconfirmed acknowledgement is a
	// backend failure: the lease expires and the job is redelivered.
	finish := func(result Result, failure error) bool {
		state.completion, state.requested, state.failure = result, true, failure
		outcome = jobOutcome(result)
		err := w.finalize(key, reservation.Ownership, result, validUntil)
		state.finalized = err == nil
		// finalize returns the sentinel itself; never traverse a backend error.
		if err != nil && err != ErrOwnershipLost {
			state.failure = errors.Join(failure, err)
			return false
		}
		return true
	}
	if originErr != nil {
		return finish(Result{State: Failed, Reason: PayloadInvalid}, originErr)
	}
	if run.reserve.Err() != nil {
		// Draining began before this reservation was admitted; nothing started.
		outcome = observability.Cancelled
		return finish(Result{State: Waiting, Reason: WorkerStopped}, nil)
	}
	if deadline := envelope.Policy().RetryUntil; !deadline.IsZero() && !time.Now().Before(deadline) {
		// Past its retry deadline the job is not started again.
		return finish(Result{State: Failed, Reason: RetryExpired}, nil)
	}
	entry, err := w.registry.lookup(jobKey{envelope.Name(), envelope.Version()})
	if err != nil || entry.prepare == nil {
		return w.unregistered(key, reservation, envelope, &state, finish)
	}
	work, cancel := context.WithCancelCause(parent)
	defer cancel(context.Canceled)
	beat := &heartbeat{stop: make(chan struct{}), done: make(chan struct{}), renewed: reservedAt}
	go w.heartbeat(parent, key, reservation.Ownership, cancel, beat)
	// Finalization begins only after all preparation, handler and middleware
	// callbacks exit. Heartbeats therefore also cover context-ignoring admission.
	complete := func(result Result, failure error) bool {
		close(beat.stop)
		<-beat.done
		validUntil = beat.renewed.Add(w.config.LeaseDuration)
		if beat.err != nil {
			state.completion, state.requested, state.failure = result, true, errors.Join(failure, beat.err)
			outcome = jobOutcome(result)
			return true
		}
		return finish(result, failure)
	}
	invocation, stopTimeout := context.WithTimeout(work, envelope.Policy().Timeout)
	defer stopTimeout()
	frame := &executionFrame{worker: w, typ: entry.typ}
	frame.active.Store(true)
	prepareContext := context.WithValue(invocation, executionKey{}, frame)
	var prepared preparedJob
	var invalidPayload bool
	prepareErr := callback.Isolated("prepare job", func() error {
		var err error
		prepared, err = entry.prepare(prepareContext, envelope)
		invalidPayload = errorgraph.Has[*payloadError](err)
		return err
	})
	frame.active.Store(false)
	// A resource held since preparation (an overlap lease) is freed even when
	// the attempt never starts; after invocation this is a no-op.
	if prepared.release != nil {
		defer func() {
			_ = callback.Isolated("release job preparation", func() error { prepared.release(); return nil })
		}()
	}
	invoke, delay := prepared.invoke, prepared.delay
	if invalidPayload {
		return complete(Result{State: Failed, Reason: PayloadInvalid}, prepareErr)
	}
	if delay > 0 && prepareErr == nil && invocation.Err() == nil {
		return complete(Result{State: Waiting, Delay: delay, Reason: RateLimited}, nil)
	}
	if parent.Err() != nil || run.reserve.Err() != nil || errors.Is(context.Cause(invocation), ErrCancelled) || errors.Is(context.Cause(invocation), ErrOwnershipLost) {
		return complete(Result{State: Waiting, Reason: WorkerStopped}, nil)
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
			// The start may have applied before its reply was lost: refund it.
			// Backends ignore the refund for a record that is still reserved.
			return complete(Result{State: Waiting, Reason: WorkerStopped, Refund: true}, nil)
		}
		// Start may or may not have applied. Refund keeps an unstarted handler
		// from consuming budget; the backend ignores it for a reserved record.
		w.logBackendFailure(parent, "start", key, startErr)
		complete(Result{State: Waiting, Reason: WorkerStopped, Refund: true}, nil)
		outcome, state.failure, state.requested = observability.Failed, startErr, false
		return false
	}
	if attempt == 0 || attempt > envelope.Policy().Attempts || attempt != reservation.Attempts+1 {
		invalid := fault.New(fault.Internal, "backend returned an invalid job attempt")
		w.logBackendFailure(parent, "start", key, invalid)
		complete(Result{State: Waiting, Reason: WorkerStopped, Refund: true}, nil)
		outcome, state.failure, state.requested = observability.Failed, invalid, false
		return false
	}
	state.number = attempt
	frame = &executionFrame{worker: w, typ: entry.typ, attempt: Attempt{ID: envelope.ID(), Name: envelope.Name(), Version: envelope.Version(), Queue: envelope.Queue(), Number: attempt, Retry: reservation.Retries}}
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
	result := safeExecutionResult(envelope.Policy(), attempt, reservation.Exceptions, handlerErr, cause, parent.Err() != nil)
	if frame.noRetry.Load() && result.State == Waiting && result.Reason != CancelRequested {
		result.State, result.Delay, result.Refund = Failed, 0, false
	}
	frame.active.Store(false)
	return complete(result, handlerErr)
}

// unregistered handles a name/version this process does not declare. During a
// rolling deploy another replica may know it, so the default policy consumes
// attempts with the envelope's own backoff before retaining it as a failure.
func (w *Worker) unregistered(key Key, reservation Reservation, envelope Envelope, state *attemptState, finish func(Result, error) bool) bool {
	missing := fault.New(fault.Missing, "job name or version is not registered")
	if !w.config.RetryUnregistered {
		return finish(Result{State: Failed, Reason: Unregistered}, missing)
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.config.OperationTimeout)
	var attempt uint32
	err := callback.Isolated("start job", func() error {
		var err error
		attempt, err = w.backend.JobStart(ctx, key, reservation.Ownership)
		return err
	})
	cancel()
	if err != nil {
		return finish(Result{State: Waiting, Reason: Unregistered, Refund: true}, errors.Join(missing, err))
	}
	state.number = attempt
	policy := envelope.Policy()
	if attempt >= policy.Attempts {
		return finish(Result{State: Failed, Reason: Unregistered}, missing)
	}
	delay, err := policy.RetryDelay(attempt)
	if err != nil {
		return finish(Result{State: Failed, Reason: Unregistered}, errors.Join(missing, err))
	}
	return finish(Result{State: Waiting, Reason: Unregistered, Delay: policy.jittered(delay)}, missing)
}

// heartbeat renews one reservation. A failed renewal leaves ownership unknown,
// so it keeps retrying until the lease would really have expired; only then is
// the handler cancelled. Loss affects this job only, never other reservations.
type heartbeat struct {
	stop    chan struct{}
	done    chan struct{}
	renewed time.Time
	err     error
}

func (w *Worker) heartbeat(ctx context.Context, key Key, proof Ownership, cancel context.CancelCauseFunc, beat *heartbeat) {
	defer close(beat.done)
	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()
	failing := false
	for {
		select {
		case <-beat.stop:
			return
		case <-ticker.C:
		}
		started := time.Now()
		operation, done := context.WithTimeout(context.Background(), w.config.OperationTimeout)
		var status LeaseStatus
		err := callback.Isolated("renew job reservation", func() error {
			var err error
			status, err = w.backend.JobRenew(operation, key, proof, w.config.LeaseDuration)
			return err
		})
		done()
		switch {
		case err == nil && !status.Owned:
			beat.err = ErrOwnershipLost
			cancel(ErrOwnershipLost)
			return
		case err == nil:
			beat.renewed, failing = started, false
			if status.CancellationRequested {
				cancel(ErrCancelled)
			}
		default:
			if !failing {
				w.logBackendFailure(ctx, "renew", key, err)
				failing = true
			}
			if time.Since(beat.renewed) >= w.config.LeaseDuration {
				beat.err = ErrOwnershipLost
				cancel(ErrOwnershipLost)
				return
			}
		}
	}
}

// finalize retries an unconfirmed acknowledgement with jittered backoff while
// the reservation can still be valid. A retry after an applied-but-lost reply
// reports ErrOwnershipLost, which the caller logs as unconfirmed.
func (w *Worker) finalize(key Key, proof Ownership, result Result, validUntil time.Time) error {
	margin := w.config.OperationTimeout
	for failures := 1; ; failures++ {
		ctx, cancel := context.WithTimeout(context.Background(), w.config.OperationTimeout)
		var owned bool
		err := callback.Isolated("finish job", func() error {
			var err error
			owned, err = w.backend.JobFinish(ctx, key, proof, result)
			return err
		})
		cancel()
		if err == nil {
			if !owned {
				return ErrOwnershipLost
			}
			return nil
		}
		delay := w.failureDelay(failures)
		if time.Now().Add(delay + margin).After(validUntil) {
			return err
		}
		time.Sleep(delay)
	}
}

// Error Is/As/Unwrap are extension callbacks too. Keep the execution frame and
// heartbeat alive until classification actually returns, including Goexit.
func safeExecutionResult(policy Policy, attempt, exceptions uint32, err, cause error, stopping bool) Result {
	var result Result
	failed := callback.Isolated("classify job outcome", func() error {
		result = limitExceptions(policy, exceptions, executionResult(policy, attempt, err, cause, stopping))
		return nil
	})
	if failed != nil {
		return limitExceptions(policy, exceptions, executionResult(policy, attempt, fault.New(fault.Panicked, "job outcome classifier failed"), nil, stopping))
	}
	return result
}

// limitExceptions applies Policy.MaxExceptions and Policy.RetryUntil to a
// classified attempt. exceptions counts earlier exception attempts.
func limitExceptions(policy Policy, exceptions uint32, result Result) Result {
	if result.State != Waiting || result.Refund {
		return result
	}
	if policy.MaxExceptions > 0 && result.Reason.exception() && exceptions+1 >= policy.MaxExceptions {
		return Result{State: Failed, Reason: ExceptionLimit}
	}
	if !policy.RetryUntil.IsZero() && time.Now().Add(result.Delay).After(policy.RetryUntil) {
		return Result{State: Failed, Reason: RetryExpired}
	}
	return result
}

// executionResult classifies only failures: a handler that returned nil
// succeeded, even if the worker began stopping or the deadline passed after its
// side effects. An interrupted attempt during shutdown is released and refunded.
func executionResult(policy Policy, attempt uint32, err, cause error, stopping bool) Result {
	if err == nil {
		return Result{State: Succeeded}
	}
	reason := HandlerFailed
	switch {
	case errors.Is(cause, ErrCancelled):
		reason = CancelRequested
	case errorgraph.Has[*permanentError](err):
		return Result{State: Failed, Reason: HandlerFailed}
	case errors.Is(cause, context.DeadlineExceeded):
		reason = TimedOut
	case stopping:
		return Result{State: Waiting, Reason: WorkerStopped, Refund: true}
	case errorgraph.Has[*payloadError](err):
		return Result{State: Failed, Reason: PayloadInvalid}
	case errorgraph.Is(err, fault.Panicked):
		reason = HandlerPanicked
	}
	if attempt >= policy.Attempts {
		return Result{State: Failed, Reason: reason}
	}
	delay, _ := policy.RetryDelay(attempt)
	return Result{State: Waiting, Reason: reason, Delay: policy.jittered(delay)}
}
