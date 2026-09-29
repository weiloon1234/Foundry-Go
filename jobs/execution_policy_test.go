package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	leasememory "github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	limitmemory "github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func overlapLeases(t *testing.T) lease.Leases[string] {
	t.Helper()
	backend, err := leasememory.New(64)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := lease.NewManager(backend, lease.DefaultConfig(keyspace.Namespace{Application: "jobs", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()); backend.Close() })
	leases, err := lease.Define("jobs.overlap", keyspace.StringKeys[string]()).Bind(manager)
	if err != nil {
		t.Fatal(err)
	}
	return leases
}

// withOptions rebuilds the fixture worker with handler options and concurrency.
func withOptions(t *testing.T, f workerFixture, handler jobs.Handler[payload], options jobs.HandlerOptions[payload], concurrency int) workerFixture {
	t.Helper()
	declaration, err := f.definition.DeclareWith(handler, options)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency, config.PollInterval = concurrency, time.Millisecond
	config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 150*time.Millisecond, 15*time.Millisecond, 30*time.Millisecond
	if f.worker, err = jobs.NewWorker(f.backend, registry, config); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWithoutOverlappingSerializesSameKeyAndReleasesOnExit(t *testing.T) {
	for _, mode := range []jobs.OverlapMode{jobs.ReleaseOnOverlap, jobs.SkipOnOverlap} {
		t.Run(map[jobs.OverlapMode]string{jobs.ReleaseOnOverlap: "release", jobs.SkipOnOverlap: "skip"}[mode], func(t *testing.T) {
			var running, maximum, calls atomic.Int32
			entered, release := make(chan struct{}, 4), make(chan struct{})
			options := jobs.OverlapOptions{TTL: time.Second, Delay: 5 * time.Millisecond, Mode: mode}
			f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
			f = withOptions(t, f, func(context.Context, payload) error {
				calls.Add(1)
				if current := running.Add(1); current > maximum.Load() {
					maximum.Store(current)
				}
				entered <- struct{}{}
				<-release
				running.Add(-1)
				return nil
			}, jobs.HandlerOptions[payload]{Overlap: jobs.WithoutOverlapping(overlapLeases(t), func(p payload) string { return p.Labels["account"] }, options)}, 2)
			dispatch := func() jobs.ID[payload] {
				receipt, err := f.definition.Dispatch(t.Context(), f.dispatcher, payload{Labels: map[string]string{"account": "same"}}, jobs.Options[payload]{})
				if err != nil {
					t.Fatal(err)
				}
				return receipt.ID
			}
			first := dispatch()
			runWorker(t, f)
			<-entered
			second := dispatch()
			if mode == jobs.SkipOnOverlap {
				// The overlapping job completes without running.
				waitRecord(t, f, second, jobs.Succeeded)
			} else {
				time.Sleep(50 * time.Millisecond)
			}
			close(release)
			waitRecord(t, f, first, jobs.Succeeded)
			record := waitRecord(t, f, second, jobs.Succeeded)
			if maximum.Load() != 1 {
				t.Fatal("jobs with the same key overlapped")
			}
			want := int32(2)
			if mode == jobs.SkipOnOverlap {
				want = 1
			}
			if calls.Load() != want || record.Attempts > 1 {
				t.Fatal("overlap handling", calls.Load(), record.Attempts)
			}
			// The lease was released after exit: a later job with the key runs.
			waitRecord(t, f, dispatch(), jobs.Succeeded)
		})
	}
	if _, err := jobs.Define[payload]("invalid.overlap", 1, jobs.DefaultPolicy("default")).DeclareWith(func(context.Context, payload) error { return nil }, jobs.HandlerOptions[payload]{
		Overlap: jobs.WithoutOverlapping(overlapLeases(t), func(payload) string { return "key" }, jobs.OverlapOptions{TTL: time.Second}),
	}); err == nil {
		t.Fatal("release mode without delay accepted")
	}
}

func TestThrottleExceptionsReleasesWithoutConsumingAttempts(t *testing.T) {
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
	limiter, err := ratelimit.Define("jobs.exceptions", keyspace.StringKeys[string](), ratelimit.PerHour(1)).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	policy := jobs.DefaultPolicy("default")
	policy.Attempts, policy.Backoff, policy.Jitter = 5, []time.Duration{time.Millisecond}, 0
	f := newWorkerFixture(t, policy, nil)
	f = withOptions(t, f, func(context.Context, payload) error { calls.Add(1); return errors.New("provider down") }, jobs.HandlerOptions[payload]{
		Throttle: jobs.ThrottleExceptions(limiter, func(payload) string { return "provider" }, 10*time.Millisecond),
	}, 1)
	id := f.enqueue(t)
	runWorker(t, f)
	// After one exception the budget is exhausted: later reservations are
	// released as rate limited while the attempt count stays at one.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found, err := f.definition.Inspect(t.Context(), f.dispatcher, id, "")
		if err != nil {
			t.Fatal(err)
		}
		record, _ := found.Get()
		if len(record.History) > 0 && record.History[len(record.History)-1].Reason == jobs.RateLimited {
			if record.Attempts != 1 || calls.Load() != 1 {
				t.Fatal("throttled job consumed attempts", record.Attempts, calls.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exception throttle did not release the job")
}

func TestMaxExceptionsAndRetryUntilEndRetries(t *testing.T) {
	t.Run("max-exceptions", func(t *testing.T) {
		policy := jobs.DefaultPolicy("default")
		policy.Attempts, policy.MaxExceptions, policy.Backoff, policy.Jitter = 5, 2, []time.Duration{time.Millisecond}, 0
		f := newWorkerFixture(t, policy, func(context.Context, payload) error { return errors.New("fail") })
		id := f.enqueue(t)
		runWorker(t, f)
		record := waitRecord(t, f, id, jobs.Failed)
		if record.Attempts != 2 || record.Exceptions != 2 || record.History[len(record.History)-1].Reason != jobs.ExceptionLimit {
			t.Fatalf("exception limit: attempts=%d exceptions=%d %+v", record.Attempts, record.Exceptions, record.History)
		}
	})
	t.Run("retry-until-passed", func(t *testing.T) {
		var calls atomic.Int32
		f := newWorkerFixture(t, jobs.DefaultPolicy("default"), func(context.Context, payload) error { calls.Add(1); return nil })
		receipt, err := f.definition.Dispatch(t.Context(), f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{RetryUntil: time.Now().Add(-time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		runWorker(t, f)
		record := waitRecord(t, f, receipt.ID, jobs.Failed)
		if record.Attempts != 0 || calls.Load() != 0 || record.History[len(record.History)-1].Reason != jobs.RetryExpired {
			t.Fatal("expired job started", record.Attempts, calls.Load())
		}
	})
	t.Run("retry-beyond-deadline", func(t *testing.T) {
		policy := jobs.DefaultPolicy("default")
		policy.Backoff, policy.Jitter = []time.Duration{time.Hour}, 0
		f := newWorkerFixture(t, policy, func(context.Context, payload) error { return errors.New("fail") })
		receipt, err := f.definition.Dispatch(t.Context(), f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{RetryUntil: time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		runWorker(t, f)
		record := waitRecord(t, f, receipt.ID, jobs.Failed)
		if record.Attempts != 1 || record.History[len(record.History)-1].Reason != jobs.RetryExpired {
			t.Fatal("retry scheduled past its deadline", record.Attempts)
		}
	})
}

func TestExtendedEnvelopeVersionIsSelectedOnlyForNewFields(t *testing.T) {
	plain := jobs.Define[payload]("plain", 1, jobs.DefaultPolicy("default"))
	pending, err := plain.Capture(t.Context(), payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil || pending.Envelope().WireVersion() != jobs.LegacyEnvelope {
		t.Fatal("plain envelope changed format", err)
	}
	policy := jobs.DefaultPolicy("default")
	policy.MaxExceptions = 2
	extended := jobs.Define[payload]("extended", 1, policy)
	pending, err = extended.Capture(t.Context(), payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil || pending.Envelope().WireVersion() != jobs.ExtendedEnvelope {
		t.Fatal("extended policy did not select format 3", err)
	}
	data, err := pending.Envelope().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jobs.DecodeEnvelope(data)
	if err != nil || decoded.Policy().MaxExceptions != 2 {
		t.Fatal("extended envelope did not round-trip", err)
	}
	// Extended fields in an older format are rejected rather than ignored.
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	delete(wire, "envelope_version")
	downgraded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.DecodeEnvelope(downgraded); err == nil {
		t.Fatal("extended fields accepted in the legacy format")
	}
	key, err := jobs.NewUniqueKey[payload]("once")
	if err != nil {
		t.Fatal(err)
	}
	pending, err = plain.Capture(t.Context(), payload{Labels: map[string]string{}}, jobs.Options[payload]{Unique: jobs.Unique[payload]{Key: key, For: time.Hour, UntilProcessing: true}})
	if err != nil || pending.Envelope().WireVersion() != jobs.ExtendedEnvelope || !pending.Envelope().Uniqueness().UntilProcessing {
		t.Fatal("until-processing uniqueness did not select format 3", err)
	}
}
