package jobtest

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func manualRetry(t *testing.T, makeFixture func(*testing.T) Fixture) {
	f := makeFixture(t)
	backend, ok := f.Backend.(jobs.RetryBackend)
	if !ok {
		t.Fatal("built-in authority lacks retries")
	}
	d := jobs.Define[Payload]("retry.work", 1, jobs.DefaultPolicy(f.Key.Queue()))
	pending, err := d.Capture(t.Context(), Payload{Exact: ^uint64(0), Text: "private-retry-payload"}, jobs.Options[Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, pending.Envelope()); err != nil {
		t.Fatal(err)
	}
	read := func() jobs.Record {
		t.Helper()
		found, err := f.Backend.JobInspect(t.Context(), f.Key, pending.Envelope().ID())
		if err != nil {
			t.Fatal(err)
		}
		r, ok := found.Get()
		if !ok {
			t.Fatal("record missing")
		}
		return r
	}
	if _, err := read().RetryToken(); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatal("waiting job retryable", err)
	}
	finish := func(state jobs.State) jobs.Reservation {
		t.Helper()
		owner, err := lease.NewOwner()
		if err != nil {
			t.Fatal(err)
		}
		found, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		claim, ok := found.Get()
		if !ok {
			t.Fatal("job not reservable")
		}
		if attempt, err := f.Backend.JobStart(t.Context(), f.Key, claim.Ownership); err != nil || attempt != 1 {
			t.Fatal("new retry budget", attempt, err)
		}
		reason := jobs.NoReason
		if state == jobs.Failed {
			reason = jobs.HandlerFailed
		}
		if changed, err := f.Backend.JobFinish(t.Context(), f.Key, claim.Ownership, jobs.Result{State: state, Reason: reason}); err != nil || !changed {
			t.Fatal(changed, err)
		}
		return claim
	}
	oldOwner := finish(jobs.Failed)
	before := read()
	token, err := before.RetryToken()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(before.Summary())
	if err != nil || strings.Contains(string(encoded), "private-retry-payload") {
		t.Fatal("summary exposed payload", err)
	}
	request := jobs.RetryRequest{Target: pending.Envelope().Target(), Token: token}
	var won atomic.Int32
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			changed, err := backend.JobRetry(t.Context(), f.Key, request)
			if err != nil {
				t.Error(err)
			}
			if changed {
				won.Add(1)
			}
		})
	}
	group.Wait()
	if won.Load() != 1 {
		t.Fatal("retry applied more than once", won.Load())
	}
	after := read()
	if after.State != jobs.Waiting || after.Attempts != 0 || after.Retries != 1 || after.LastRetry != token || !after.FinishedAt.IsZero() || after.Envelope.PayloadJSON() != before.Envelope.PayloadJSON() || after.CreatedAt != before.CreatedAt {
		t.Fatal("retry changed captured identity or lost budget/history")
	}
	last := after.History[len(after.History)-1]
	if last.Reason != jobs.ManuallyRetried || last.Retry != 1 || len(after.History) < 2 || after.History[len(after.History)-2].Reason != jobs.HandlerFailed {
		t.Fatal("retry history missing")
	}
	if changed, err := f.Backend.JobFinish(t.Context(), f.Key, oldOwner.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || changed {
		t.Fatal("old lease mutated retried job", changed, err)
	}
	if inserted, err := f.Backend.JobEnqueue(t.Context(), f.Key, pending.Envelope()); err != nil || inserted {
		t.Fatal("outbox identity deduplication changed", inserted, err)
	}
	finish(jobs.Failed)
	if changed, err := backend.JobRetry(t.Context(), f.Key, request); err != nil || changed || read().State != jobs.Failed {
		t.Fatal("ambiguous retry reopened a later failure", changed, err)
	}
	newToken, err := read().RetryToken()
	if err != nil || newToken == token {
		t.Fatal("failed generation did not change", err)
	}
	newRequest := jobs.RetryRequest{Target: request.Target, Token: newToken}
	if changed, err := backend.JobRetry(t.Context(), f.Key, newRequest); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := backend.JobRetry(t.Context(), f.Key, request); !errors.Is(err, jobs.ErrNotRetryable) || changed {
		t.Fatal("stale token accepted", err)
	}
	finish(jobs.Succeeded)
	if _, err := read().RetryToken(); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatal("successful job retryable", err)
	}
	if changed, err := backend.JobRetry(t.Context(), f.Key, newRequest); err != nil || changed {
		t.Fatal("accepted token did not reconcile", err)
	}
	foreign := newRequest
	foreign.Target.Name = "other.work"
	if changed, err := backend.JobRetry(t.Context(), f.Key, foreign); !errors.Is(err, jobs.ErrNotRetryable) || changed {
		t.Fatal("foreign job target accepted", err)
	}
	f.Advance(8 * 24 * time.Hour)
	if changed, err := backend.JobRetry(t.Context(), f.Key, newRequest); !errors.Is(err, jobs.ErrNotRetryable) || changed {
		t.Fatal("expired retry resurrected work", err)
	}

	// Workflows have already changed their dependants; retry cannot reopen one
	// failed member while leaving chain/batch completion permanently inconsistent.
	groupPending, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := jobs.NewChain(groupPending.Step())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, workflow.Envelope()); err != nil {
		t.Fatal(err)
	}
	finish(jobs.Failed)
	found, err := f.Backend.JobInspect(t.Context(), f.Key, groupPending.Envelope().ID())
	if err != nil {
		t.Fatal(err)
	}
	member, ok := found.Get()
	if !ok {
		t.Fatal("workflow member missing")
	}
	if _, err := member.RetryToken(); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatal("workflow member reopened", err)
	}
	if _, err := backend.JobRetry(t.Context(), f.Key, jobs.RetryRequest{Target: member.Envelope.Target(), Token: token}); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatal(err)
	}

	// Unknown-version/payload preparation failures can be terminal at attempt
	// zero. A later registered handler can replay the same retained envelope.
	unstarted, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, unstarted.Envelope()); err != nil {
		t.Fatal(err)
	}
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok := reservation.Get()
	if !ok {
		t.Fatal("unstarted reservation missing")
	}
	if _, err := f.Backend.JobFinish(t.Context(), f.Key, claim.Ownership, jobs.Result{State: jobs.Failed, Reason: jobs.Unregistered}); err != nil {
		t.Fatal(err)
	}
	failed, err := f.Backend.JobInspect(t.Context(), f.Key, unstarted.Envelope().ID())
	if err != nil {
		t.Fatal(err)
	}
	zero, ok := failed.Get()
	if !ok || zero.Attempts != 0 {
		t.Fatal("preparation failure counted as a started attempt")
	}
	zeroToken, err := zero.RetryToken()
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := backend.JobRetry(t.Context(), f.Key, jobs.RetryRequest{Target: zero.Envelope.Target(), Token: zeroToken}); err != nil || !changed {
		t.Fatal("zero-attempt failure could not be retried", err)
	}
	zero.Retries = jobs.MaxManualRetries
	if _, err := zero.RetryToken(); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatal("manual cycle limit ignored", err)
	}
}
