package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type payload struct {
	Number int               `json:"number"`
	Labels map[string]string `json:"labels"`
}
type fixture struct {
	backend    *memory.Backend
	clock      *testkit.Clock
	key        jobs.Key
	definition jobs.Definition[payload]
}

func setup(t *testing.T, attempts uint32) fixture {
	t.Helper()
	c := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	config := memory.DefaultConfig()
	config.Clock = c
	config.MaxHistory = 4
	config.Retention = time.Hour
	backend, err := memory.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	key, err := jobs.NewKey(keyspace.Namespace{Application: "jobs", Environment: "test"}, "default")
	if err != nil {
		t.Fatal(err)
	}
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = attempts
	policy.Jitter = 0
	return fixture{backend: backend, clock: c, key: key, definition: jobs.Define[payload]("work", 1, policy)}
}
func (f fixture) enqueue(t *testing.T, number int) jobs.Envelope {
	t.Helper()
	pending, err := f.definition.Capture(context.Background(), payload{Number: number, Labels: map[string]string{"source": "original"}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	added, err := f.backend.JobEnqueue(context.Background(), f.key, pending.Envelope())
	if err != nil || !added {
		t.Fatalf("enqueue: %v %v", added, err)
	}
	return pending.Envelope()
}
func (f fixture) reserve(t *testing.T) jobs.Reservation {
	t.Helper()
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := f.backend.JobReserve(context.Background(), f.key, owner, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := found.Get()
	if !ok {
		t.Fatal("expected reservation")
	}
	return result
}
func (f fixture) record(t *testing.T, id jobs.ExecutionID) jobs.Record {
	t.Helper()
	found, err := f.backend.JobInspect(context.Background(), f.key, id)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := found.Get()
	if !ok {
		t.Fatal("expected retained job")
	}
	return result
}
func TestLeaseExpiryRedeliveryAndStaleOwners(t *testing.T) {
	f := setup(t, 3)
	message := f.enqueue(t, 1)
	first := f.reserve(t)
	if attempt, err := f.backend.JobStart(context.Background(), f.key, first.Ownership); err != nil || attempt != 1 {
		t.Fatalf("start: %d %v", attempt, err)
	}
	if attempt, err := f.backend.JobStart(context.Background(), f.key, first.Ownership); err != nil || attempt != 1 {
		t.Fatalf("idempotent start: %d %v", attempt, err)
	}
	f.clock.Advance(time.Second)
	second := f.reserve(t)
	if first.Ownership.Owner() == second.Ownership.Owner() || second.Envelope.ID() != message.ID() {
		t.Fatal("redelivery must keep identity and replace ownership")
	}
	if second.Attempts != 1 {
		t.Fatal("redelivery reset attempts")
	}
	if status, err := f.backend.JobRenew(context.Background(), f.key, first.Ownership, time.Second); err != nil || status.Owned {
		t.Fatalf("stale renew: %+v %v", status, err)
	}
	if _, err := f.backend.JobStart(context.Background(), f.key, first.Ownership); !errors.Is(err, jobs.ErrOwnershipLost) {
		t.Fatalf("stale start: %v", err)
	}
	if ok, err := f.backend.JobFinish(context.Background(), f.key, first.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || ok {
		t.Fatalf("stale finish: %v %v", ok, err)
	}
	if attempt, err := f.backend.JobStart(context.Background(), f.key, second.Ownership); err != nil || attempt != 2 {
		t.Fatalf("second start: %d %v", attempt, err)
	}
	if ok, err := f.backend.JobFinish(context.Background(), f.key, second.Ownership, jobs.Result{State: jobs.Waiting, Delay: time.Minute, Reason: jobs.HandlerFailed}); err != nil || !ok {
		t.Fatal(err)
	}
	owner, _ := lease.NewOwner()
	if found, err := f.backend.JobReserve(context.Background(), f.key, owner, time.Second); err != nil || found.IsSet() {
		t.Fatalf("early retry: %v", err)
	}
	f.clock.Advance(time.Minute)
	third := f.reserve(t)
	if attempt, err := f.backend.JobStart(context.Background(), f.key, third.Ownership); err != nil || attempt != 3 {
		t.Fatalf("third start: %d %v", attempt, err)
	}
	f.clock.Advance(time.Second)
	record := f.record(t, message.ID())
	if record.State != jobs.Failed || record.Attempts != 3 || len(record.History) != 4 || record.History[3].Reason != jobs.AttemptLimit {
		t.Fatalf("crash exhaustion: %+v", record)
	}
	record.History[0].State = jobs.Succeeded
	if f.record(t, message.ID()).History[0].State == jobs.Succeeded {
		t.Fatal("history alias escaped")
	}
}
func TestCancellationFencesSchemaAndKeepsRunningLease(t *testing.T) {
	f := setup(t, 2)
	message := f.enqueue(t, 1)
	claim := f.reserve(t)
	if _, err := f.backend.JobStart(context.Background(), f.key, claim.Ownership); err != nil {
		t.Fatal(err)
	}
	wrong := message.Target()
	wrong.Name = "other"
	if ok, err := f.backend.JobCancel(context.Background(), f.key, wrong); err != nil || ok {
		t.Fatal("foreign target canceled job")
	}
	if ok, err := f.backend.JobCancel(context.Background(), f.key, message.Target()); err != nil || !ok {
		t.Fatal(err)
	}
	status, err := f.backend.JobRenew(context.Background(), f.key, claim.Ownership, time.Second)
	if err != nil || !status.Owned || !status.CancellationRequested {
		t.Fatalf("cancel must retain ownership: %+v %v", status, err)
	}
	if record := f.record(t, message.ID()); record.State != jobs.Running {
		t.Fatal("cancellation released a running handler")
	}
	if ok, err := f.backend.JobFinish(context.Background(), f.key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
		t.Fatal(err)
	}
	if f.record(t, message.ID()).State != jobs.Cancelled {
		t.Fatal("cancellation lost to acknowledgement")
	}
	queued := f.enqueue(t, 2)
	if ok, err := f.backend.JobCancel(context.Background(), f.key, queued.Target()); err != nil || !ok {
		t.Fatal(err)
	}
	if f.record(t, queued.ID()).State != jobs.Cancelled {
		t.Fatal("waiting cancellation did not finish")
	}
}
func TestConcurrentReservationIsAtomic(t *testing.T) {
	f := setup(t, 2)
	f.enqueue(t, 1)
	var won atomic.Int64
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			owner, err := lease.NewOwner()
			if err != nil {
				t.Error(err)
				return
			}
			found, err := f.backend.JobReserve(context.Background(), f.key, owner, time.Second)
			if err != nil {
				t.Error(err)
				return
			}
			if found.IsSet() {
				won.Add(1)
			}
		})
	}
	group.Wait()
	if won.Load() != 1 {
		t.Fatalf("reservations = %d", won.Load())
	}
}
func TestDeduplicationConflictAndRetention(t *testing.T) {
	f := setup(t, 1)
	message := f.enqueue(t, 1)
	if ok, err := f.backend.JobEnqueue(context.Background(), f.key, message); err != nil || ok {
		t.Fatalf("duplicate: %v %v", ok, err)
	}
	id, err := jobs.ParseID[payload](message.ID().String())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := f.definition.Capture(context.Background(), payload{Number: 2, Labels: map[string]string{}}, jobs.Options[payload]{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.backend.JobEnqueue(context.Background(), f.key, changed.Envelope()); !errors.Is(err, fault.Conflict) {
		t.Fatalf("identity conflict: %v", err)
	}
	claim := f.reserve(t)
	if _, err := f.backend.JobStart(context.Background(), f.key, claim.Ownership); err != nil {
		t.Fatal(err)
	}
	if _, err := f.backend.JobFinish(context.Background(), f.key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.backend.JobEnqueue(context.Background(), f.key, message); err != nil || ok {
		t.Fatal("terminal identity was not retained")
	}
	f.clock.Advance(time.Hour)
	if ok, err := f.backend.JobEnqueue(context.Background(), f.key, message); err != nil || !ok {
		t.Fatalf("retention expiration: %v %v", ok, err)
	}
}
func TestCapacityNeverEvictsWork(t *testing.T) {
	f := setup(t, 2)
	config := memory.DefaultConfig()
	config.MaxEntries = 1
	config.Clock = f.clock
	backend, err := memory.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	f.backend = backend
	first := f.enqueue(t, 1)
	other, err := f.definition.Capture(context.Background(), payload{Number: 2, Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	// A full queue is temporary exhaustion: retryable, never a conflict.
	if _, err := backend.JobEnqueue(context.Background(), f.key, other.Envelope()); !errors.Is(err, fault.Overloaded) || !errors.Is(err, jobs.ErrQueueFull) {
		t.Fatalf("capacity: %v", err)
	}
	if f.record(t, first.ID()).State != jobs.Waiting {
		t.Fatal("unfinished work evicted")
	}
	if ok, err := backend.JobEnqueue(context.Background(), f.key, first); err != nil || ok {
		t.Fatal("duplicate should fit a full authority")
	}
}
func TestExpiredOwnerCannotBeResurrectedByBackwardClock(t *testing.T) {
	f := setup(t, 2)
	message := f.enqueue(t, 1)
	old := f.reserve(t)
	f.clock.Advance(time.Second)
	_ = f.record(t, message.ID()) // Observe expiry before the clock moves backward.
	f.clock.Advance(-time.Hour)
	status, err := f.backend.JobRenew(context.Background(), f.key, old.Ownership, time.Second)
	if err != nil || status.Owned {
		t.Fatalf("expired owner resurrected: %+v %v", status, err)
	}
	_ = f.reserve(t)
}

// finish reserves, starts and completes the next job with state.
func (f fixture) finish(t *testing.T, state jobs.State) jobs.Reservation {
	t.Helper()
	reservation := f.reserve(t)
	if _, err := f.backend.JobStart(context.Background(), f.key, reservation.Ownership); err != nil {
		t.Fatal(err)
	}
	result := jobs.Result{State: state}
	if state == jobs.Failed {
		result.Reason = jobs.HandlerFailed
	}
	if ok, err := f.backend.JobFinish(context.Background(), f.key, reservation.Ownership, result); err != nil || !ok {
		t.Fatal("finish", ok, err)
	}
	return reservation
}

func TestCapacityCountsOnlyLiveWorkAndEvictsOldestTerminal(t *testing.T) {
	f := setup(t, 1)
	config := memory.DefaultConfig()
	config.MaxEntries, config.MaxRetained, config.Retention = 2, 2, time.Hour
	config.Clock = f.clock
	backend, err := memory.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	f.backend = backend
	var finished []jobs.ExecutionID
	for i := range 4 {
		envelope := f.enqueue(t, i)
		f.finish(t, jobs.Succeeded)
		finished = append(finished, envelope.ID())
		f.clock.Advance(time.Millisecond)
	}
	// Retained successes never block new work; the oldest were evicted.
	for _, id := range finished[:2] {
		if found, err := backend.JobInspect(context.Background(), f.key, id); err != nil || found.IsSet() {
			t.Fatal("oldest terminal record was not evicted", err)
		}
	}
	for _, id := range finished[2:] {
		if f.record(t, id).State != jobs.Succeeded {
			t.Fatal("newest terminal records were not retained")
		}
	}
	f.enqueue(t, 10)
	f.enqueue(t, 11)
	stats, err := backend.JobStats(context.Background(), f.key)
	if err != nil || stats.Waiting != 2 || stats.Retained != 2 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
	extra, err := f.definition.Capture(context.Background(), payload{Number: 12, Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.JobEnqueue(context.Background(), f.key, extra.Envelope()); !errors.Is(err, jobs.ErrQueueFull) {
		t.Fatal("live capacity ignored", err)
	}
}

func TestRepeatedReservationExpiryFailsPoisonJob(t *testing.T) {
	f := setup(t, 1)
	envelope := f.enqueue(t, 1)
	for range 3 {
		// A process that crashes while decoding never starts the attempt.
		_ = f.reserve(t)
		f.clock.Advance(2 * time.Second)
		if state := f.record(t, envelope.ID()).State; state != jobs.Waiting && state != jobs.Failed {
			t.Fatal("unexpected state", state)
		}
	}
	record := f.record(t, envelope.ID())
	if record.State != jobs.Failed || record.History[len(record.History)-1].Reason != jobs.DeliveryLimit || record.Attempts != 0 {
		t.Fatalf("poison job was redelivered forever: %+v", record)
	}
}

func TestRefundedReleaseReturnsInterruptedAttempt(t *testing.T) {
	f := setup(t, 1)
	envelope := f.enqueue(t, 1)
	reservation := f.reserve(t)
	if attempt, err := f.backend.JobStart(context.Background(), f.key, reservation.Ownership); err != nil || attempt != 1 {
		t.Fatal(attempt, err)
	}
	if _, err := f.backend.JobFinish(context.Background(), f.key, reservation.Ownership, jobs.Result{State: jobs.Succeeded, Refund: true}); err == nil {
		t.Fatal("refund accepted for a terminal result")
	}
	if ok, err := f.backend.JobFinish(context.Background(), f.key, reservation.Ownership, jobs.Result{State: jobs.Waiting, Reason: jobs.WorkerStopped, Refund: true}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	record := f.record(t, envelope.ID())
	if record.State != jobs.Waiting || record.Attempts != 0 {
		t.Fatalf("interrupted attempt consumed its only budget: %+v", record)
	}
}

func TestForgetRemovesOnlyRetainedTerminalRecords(t *testing.T) {
	f := setup(t, 1)
	live := f.enqueue(t, 1)
	if changed, err := f.backend.JobForget(context.Background(), f.key, live.Target()); err != nil || changed {
		t.Fatal("live job forgotten", changed, err)
	}
	f.finish(t, jobs.Failed)
	if changed, err := f.backend.JobForget(context.Background(), f.key, live.Target()); err != nil || !changed {
		t.Fatal("failed job retained", changed, err)
	}
	if added, err := f.backend.JobEnqueue(context.Background(), f.key, live); err != nil || !added {
		t.Fatal("forgotten identity still deduplicated", added, err)
	}
}

// A finished workflow's group metadata is retained, not live: running many
// workflows to completion never exhausts live capacity.
func TestFinishedWorkflowBytesLeaveLiveCapacity(t *testing.T) {
	f := setup(t, 1)
	config := memory.DefaultConfig()
	config.MaxBytes, config.MaxRetained, config.Retention = 64<<10, 4, time.Hour
	config.Clock = f.clock
	backend, err := memory.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	f.backend = backend
	// Each finished group once kept a few hundred bytes live: well past
	// MaxBytes after this many workflows.
	for i := range 400 {
		pending, err := f.definition.Capture(context.Background(), payload{Number: i, Labels: map[string]string{}}, jobs.Options[payload]{})
		if err != nil {
			t.Fatal(err)
		}
		group, err := jobs.NewChain(pending.Step())
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := backend.JobWorkflow(context.Background(), f.key, group.Envelope()); err != nil || !ok {
			t.Fatal("finished workflows consumed live capacity", i, err)
		}
		f.finish(t, jobs.Succeeded)
		f.clock.Advance(time.Millisecond)
	}
	stats, err := backend.JobStats(context.Background(), f.key)
	if err != nil || stats.Waiting != 0 || stats.Retained > 4 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
}

// An enqueue wakes only workers subscribed to its own queue.
func TestWakeupsArePerQueue(t *testing.T) {
	f := setup(t, 1)
	other, err := jobs.NewKey(f.key.Namespace(), "other")
	if err != nil {
		t.Fatal(err)
	}
	mine, theirs := f.backend.JobWakeup(f.key), f.backend.JobWakeup(other)
	f.enqueue(t, 1)
	select {
	case <-mine:
	default:
		t.Fatal("enqueue did not wake its own queue")
	}
	select {
	case <-theirs:
		t.Fatal("enqueue woke another queue")
	default:
	}
	if f.backend.JobWakeup(other) != theirs {
		t.Fatal("an unsignalled wakeup was replaced")
	}
}
