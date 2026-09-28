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
	if _, err := backend.JobEnqueue(context.Background(), f.key, other.Envelope()); !errors.Is(err, fault.Conflict) {
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
