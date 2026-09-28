package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type uncertainRetryBackend struct {
	jobs.Backend
	retry jobs.RetryBackend
	lose  bool
}

func (b *uncertainRetryBackend) JobRetry(ctx context.Context, key jobs.Key, request jobs.RetryRequest) (bool, error) {
	changed, err := b.retry.JobRetry(ctx, key, request)
	if err == nil && b.lose {
		b.lose = false
		return false, errors.New("simulated lost acknowledgement")
	}
	return changed, err
}

func TestTypedManualRetryReconcilesAmbiguousAcceptance(t *testing.T) {
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	id := f.enqueue(t)
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := f.backend.JobReserve(t.Context(), f.key, owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok := found.Get()
	if !ok {
		t.Fatal("reservation absent")
	}
	if _, err := f.backend.JobStart(t.Context(), f.key, claim.Ownership); err != nil {
		t.Fatal(err)
	}
	if _, err := f.backend.JobFinish(t.Context(), f.key, claim.Ownership, jobs.Result{State: jobs.Failed, Reason: jobs.HandlerFailed}); err != nil {
		t.Fatal(err)
	}
	record := waitRecord(t, f, id, jobs.Failed)
	token, err := record.RetryToken()
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := f.definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend := &uncertainRetryBackend{Backend: f.backend, retry: f.backend, lose: true}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(f.key.Namespace()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.definition.Retry(t.Context(), dispatcher, id, "", token); err == nil {
		t.Fatal("unknown acceptance reported success")
	}
	if changed, err := f.definition.Retry(t.Context(), dispatcher, id, "", token); err != nil || changed {
		t.Fatal("uncertain mutation did not reconcile", err)
	}
	record = waitRecord(t, f, id, jobs.Waiting)
	if record.Retries != 1 {
		t.Fatal("unknown acceptance consumed two cycles")
	}
	// An existing third-party backend remains valid without the optional interface.
	legacy, err := jobs.NewDispatcher(struct{ jobs.Backend }{f.backend}, registry, jobs.DefaultDispatchConfig(f.key.Namespace()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.definition.Retry(t.Context(), legacy, id, "", token); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported retry silently succeeded", err)
	}
}
