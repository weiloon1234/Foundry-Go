package jobs_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	limitmemory "github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestWeightedQueuesGiveLowerPriorityBoundedService(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	namespace := keyspace.Namespace{Application: "jobs", Environment: "test"}
	var order []jobs.Queue
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var declarations []jobs.Declaration
	var definitions []jobs.Definition[payload]
	for _, queue := range []jobs.Queue{"high", "low"} {
		d := jobs.Define[payload](jobs.Name(queue), 1, jobs.DefaultPolicy(queue))
		declaration, err := d.Declare(func(handlerContext context.Context, _ payload) error {
			order = append(order, queue)
			if len(order) == 8 {
				cancel()
				<-handlerContext.Done()
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, declaration)
		definitions = append(definitions, d)
	}
	registry, err := jobs.NewRegistry(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range definitions {
		for range 8 {
			if _, err := d.Dispatch(t.Context(), dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	config := jobs.DefaultWorkerConfig(namespace)
	config.Queues = []jobs.Subscription{{Queue: "high", Weight: 3}, {Queue: "low", Weight: 1}}
	config.Concurrency = 1
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []jobs.Queue{"high", "high", "high", "low", "high", "high", "high", "low"}) {
		t.Fatal(order)
	}
}

func TestJobAdmissionUsesSharedTypedQuota(t *testing.T) {
	clock := testkit.NewClock(time.Unix(0, 0))
	backend, err := limitmemory.New(10, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	store, err := ratelimit.NewStore(backend, ratelimit.DefaultConfig(keyspace.Namespace{Application: "jobs", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.Define("delivery", keyspace.StringKeys[string](), ratelimit.PerSecond(1)).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	admit := jobs.RateLimit[payload](limiter, func(p payload) string { return p.Labels["account"] })
	p := payload{Labels: map[string]string{"account": "same"}}
	if delay, err := admit(t.Context(), p); err != nil || delay != 0 {
		t.Fatal(delay, err)
	}
	if decision, err := limiter.Allow(t.Context(), "same"); err != nil || decision.Allowed {
		t.Fatal("job did not consume shared quota", err)
	}
	if delay, err := admit(t.Context(), p); err != nil || delay != time.Second {
		t.Fatal(delay, err)
	}
	clock.Advance(time.Second)
	if delay, err := admit(t.Context(), p); err != nil || delay != 0 {
		t.Fatal(delay, err)
	}
}

type lostLeaseBackend struct {
	jobs.Backend
	finishes atomic.Int32
}

func (b *lostLeaseBackend) JobRenew(context.Context, jobs.Key, jobs.Ownership, time.Duration) (jobs.LeaseStatus, error) {
	return jobs.LeaseStatus{}, nil
}
func (b *lostLeaseBackend) JobFinish(ctx context.Context, key jobs.Key, owner jobs.Ownership, result jobs.Result) (bool, error) {
	b.finishes.Add(1)
	return b.Backend.JobFinish(ctx, key, owner, result)
}

func TestLeaseLossCancelsAndRetainsLiveHandler(t *testing.T) {
	cancelled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	d, err := f.definition.Declare(func(ctx context.Context, _ payload) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	backend := &lostLeaseBackend{Backend: f.backend}
	config := jobs.DefaultWorkerConfig(keyspace.Namespace{Application: "jobs", Environment: "test"}, "default")
	config.Concurrency = 1
	config.HeartbeatInterval = time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	f.enqueue(t)
	done := make(chan error, 1)
	go func() { done <- worker.Run(t.Context()) }()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("lease loss did not cancel handler")
	}
	if worker.Active() != 1 {
		t.Fatal("lost owner abandoned live handler")
	}
	select {
	case <-done:
		t.Fatal("worker returned before handler exit")
	default:
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, jobs.ErrOwnershipLost) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not drain")
	}
	if backend.finishes.Load() != 0 {
		t.Fatal("lost owner acknowledged work")
	}
}
