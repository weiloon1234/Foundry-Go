package jobs_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
)

type payload struct {
	Labels map[string]string `json:"labels"`
}
type otherPayload struct {
	Number int `json:"number"`
}
type user struct{}
type ownedPayload struct {
	UserID    model.ID[user] `json:"user_id"`
	CreatedAt time.Time      `json:"created_at"`
}

func TestCaptureSnapshotRegistrationAndWireRoundTrip(t *testing.T) {
	policy := jobs.DefaultPolicy("default")
	definition := jobs.Define[payload]("snapshot", 1, policy)
	policy.Backoff[0] = time.Hour
	if definition.Policy().Backoff[0] == time.Hour {
		t.Fatal("definition retained caller policy")
	}
	input := payload{Labels: map[string]string{"label": "before"}}
	pending, err := definition.Capture(context.Background(), input, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	input.Labels["label"] = "after"
	text := pending.Envelope().PayloadJSON()
	if !strings.Contains(text, "before") || strings.Contains(text, "after") {
		t.Fatal("payload snapshot changed")
	}
	encoded, err := pending.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jobs.DecodeEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID() != pending.Envelope().ID() || decoded.PayloadJSON() != text {
		t.Fatal("wire identity changed")
	}
	declaration, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.NewRegistry(declaration, declaration); !errors.Is(err, fault.Duplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := jobs.Define[ownedPayload]("owned", 1, jobs.DefaultPolicy("default")).Validate(); err != nil {
		t.Fatalf("typed IDs and time must remain ordinary payload values: %v", err)
	}
	type live struct {
		Context context.Context `json:"context"`
	}
	if err := jobs.Define[live]("live", 1, jobs.DefaultPolicy("default")).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("runtime capability accepted: %v", err)
	}
}
func TestDispatcherRejectsConflictingDefinitions(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	definition := jobs.Define[payload]("work", 1, jobs.DefaultPolicy("default"))
	declaration, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "jobs", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	other := jobs.Define[otherPayload]("work", 1, jobs.DefaultPolicy("default"))
	if _, err := other.Dispatch(context.Background(), dispatcher, otherPayload{}, jobs.Options[otherPayload]{}); !errors.Is(err, fault.Invalid) {
		t.Fatalf("foreign payload type: %v", err)
	}
	policy := definition.Policy()
	policy.Timeout = time.Second
	conflicting := jobs.Define[payload]("work", 1, policy)
	if _, err := conflicting.Dispatch(context.Background(), dispatcher, payload{}, jobs.Options[payload]{}); !errors.Is(err, fault.Invalid) {
		t.Fatalf("shadow policy: %v", err)
	}
}

type workerFixture struct {
	backend    *memory.Backend
	definition jobs.Definition[payload]
	dispatcher *jobs.Dispatcher
	worker     *jobs.Worker
	key        jobs.Key
}

func newWorkerFixture(t *testing.T, policy jobs.Policy, handler jobs.Handler[payload], options ...jobs.HandlerOptions[payload]) workerFixture {
	t.Helper()
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	definition := jobs.Define[payload]("work", 1, policy)
	var execution jobs.HandlerOptions[payload]
	if len(options) > 0 {
		execution = options[0]
	}
	declaration, err := definition.DeclareWith(handler, execution)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	namespace := keyspace.Namespace{Application: "jobs", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "default")
	config.Concurrency = 1
	config.PollInterval = time.Millisecond
	config.LeaseDuration = 150 * time.Millisecond
	config.HeartbeatInterval = 15 * time.Millisecond
	config.OperationTimeout = 30 * time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	key, err := jobs.NewKey(namespace, "default")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		_ = backend.Close()
	})
	return workerFixture{backend: backend, definition: definition, dispatcher: dispatcher, worker: worker, key: key}
}
func (f workerFixture) enqueue(t *testing.T) jobs.ID[payload] {
	t.Helper()
	receipt, err := f.definition.Dispatch(context.Background(), f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil || !receipt.Inserted {
		t.Fatalf("dispatch: %v", err)
	}
	return receipt.ID
}
func waitRecord(t *testing.T, f workerFixture, id jobs.ID[payload], state jobs.State) jobs.Record {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		found, err := f.definition.Inspect(context.Background(), f.dispatcher, id, "")
		if err != nil {
			t.Fatal(err)
		}
		if record, ok := found.Get(); ok && record.State == state {
			return record
		}
		select {
		case <-deadline.C:
			t.Fatalf("job did not reach %s", state)
			return jobs.Record{}
		case <-tick.C:
		}
	}
}
func TestWorkerRetriesKeepIdentityAndDiscardRequestContext(t *testing.T) {
	type requestKey struct{}
	var attempts atomic.Uint32
	var first jobs.ExecutionID
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 3
	policy.Backoff = []time.Duration{0}
	policy.Jitter = 0
	f := newWorkerFixture(t, policy, func(ctx context.Context, input payload) error {
		if ctx.Value(requestKey{}) != nil {
			return errors.New("request context leaked")
		}
		current, ok := jobs.Current(ctx)
		if !ok {
			return errors.New("attempt metadata missing")
		}
		number := attempts.Add(1)
		if number == 1 {
			first = current.ID
		} else if current.ID != first {
			return errors.New("redelivery identity changed")
		}
		if current.Number != number {
			return errors.New("attempt count drift")
		}
		if number < 3 {
			return errors.New("transient business failure")
		}
		return nil
	})
	id := f.enqueue(t)
	run := make(chan error, 1)
	go func() { run <- f.worker.Run(context.WithValue(context.Background(), requestKey{}, "ephemeral")) }()
	record := waitRecord(t, f, id, jobs.Succeeded)
	if record.Attempts != 3 || attempts.Load() != 3 {
		t.Fatalf("retry attempts: %d %d", record.Attempts, attempts.Load())
	}
	if err := f.worker.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-run; err != nil {
		t.Fatal(err)
	}
}
func TestUncooperativeHandlerRetainsLeaseAndShutdownOwnership(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	policy := jobs.DefaultPolicy("default")
	policy.Timeout = 5 * time.Millisecond
	policy.Attempts = 1
	f := newWorkerFixture(t, policy, func(ctx context.Context, _ payload) error { close(entered); <-release; return nil })
	id := f.enqueue(t)
	run := make(chan error, 1)
	go func() { run <- f.worker.Run(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not started")
	}
	// Cross the original lease expiry while the context-ignoring handler is live.
	<-time.After(240 * time.Millisecond)
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := f.backend.JobReserve(context.Background(), f.key, owner, time.Second)
	if err != nil || found.IsSet() {
		t.Fatalf("live handler was deliberately redelivered: %v", err)
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err = f.worker.Stop(stop)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded shutdown: %v", err)
	}
	select {
	case <-f.worker.Done():
		t.Fatal("worker abandoned live handler")
	default:
	}
	if f.worker.Active() != 1 {
		t.Fatal("live handler no longer counted")
	}
	// Release before cleanup rather than waiting forever for the owned goroutine.
	release <- struct{}{}
	select {
	case err := <-run:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not drain")
	}
	record := waitRecord(t, f, id, jobs.Failed)
	if record.History[len(record.History)-1].Reason != jobs.TimedOut {
		t.Fatalf("timeout reason: %+v", record.History)
	}
}
func TestDefinitionAndPolicyCopiesAreOwned(t *testing.T) {
	definition := jobs.Define[payload]("copy", 1, jobs.DefaultPolicy("default"))
	policy := definition.Policy()
	before := definition.Policy()
	policy.Backoff[0] = time.Hour
	if !reflect.DeepEqual(definition.Policy(), before) {
		t.Fatal("policy accessor escaped mutable defaults")
	}
}
