package jobtest

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// operations covers attempt refunds, poison-message delivery limits and the
// optional queue statistics and forget operations.
func operations(t *testing.T, makeFixture func(*testing.T) Fixture) {
	t.Run("refunded-release", func(t *testing.T) {
		f := makeFixture(t)
		policy := jobs.DefaultPolicy(f.Key.Queue())
		policy.Attempts = 1
		envelope := enqueueOne(t, f, jobs.Define[Payload]("refund.work", 1, policy))
		reservation := reserveOne(t, f)
		if attempt, err := f.Backend.JobStart(t.Context(), f.Key, reservation.Ownership); err != nil || attempt != 1 {
			t.Fatal(attempt, err)
		}
		if ok, err := f.Backend.JobFinish(t.Context(), f.Key, reservation.Ownership, jobs.Result{State: jobs.Waiting, Reason: jobs.WorkerStopped, Refund: true}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		record := inspectOne(t, f, envelope.ID())
		if record.State != jobs.Waiting || record.Attempts != 0 {
			t.Fatalf("interrupted attempt consumed budget: %s %d", record.State, record.Attempts)
		}
	})
	t.Run("delivery-limit", func(t *testing.T) {
		f := makeFixture(t)
		policy := jobs.DefaultPolicy(f.Key.Queue())
		policy.Attempts = 1
		envelope := enqueueOne(t, f, jobs.Define[Payload]("poison.work", 1, policy))
		for range 3 {
			// The process holding the reservation died before starting it.
			f.Expire(reserveOne(t, f))
		}
		record := inspectOne(t, f, envelope.ID())
		if record.State != jobs.Failed || record.Attempts != 0 || record.History[len(record.History)-1].Reason != jobs.DeliveryLimit {
			t.Fatalf("poison job was redelivered: %s %d %+v", record.State, record.Attempts, record.History)
		}
	})
	t.Run("exception-counter", func(t *testing.T) {
		f := makeFixture(t)
		envelope := enqueueOne(t, f, jobs.Define[Payload]("exception.work", 1, jobs.DefaultPolicy(f.Key.Queue())))
		for want := uint32(0); want < 2; want++ {
			reservation := reserveOne(t, f)
			if reservation.Exceptions != want {
				t.Fatalf("reservation exceptions: got %d want %d", reservation.Exceptions, want)
			}
			if _, err := f.Backend.JobStart(t.Context(), f.Key, reservation.Ownership); err != nil {
				t.Fatal(err)
			}
			if ok, err := f.Backend.JobFinish(t.Context(), f.Key, reservation.Ownership, jobs.Result{State: jobs.Waiting, Reason: jobs.HandlerFailed}); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		// A rate-limit release is not an exception.
		reservation := reserveOne(t, f)
		if ok, err := f.Backend.JobFinish(t.Context(), f.Key, reservation.Ownership, jobs.Result{State: jobs.Waiting, Reason: jobs.RateLimited}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if record := inspectOne(t, f, envelope.ID()); record.Exceptions != 2 || record.Attempts != 2 {
			t.Fatalf("exceptions=%d attempts=%d", record.Exceptions, record.Attempts)
		}
	})
	t.Run("unique-until-processing", func(t *testing.T) {
		f := makeFixture(t)
		definition := jobs.Define[Payload]("unique.processing", 1, jobs.DefaultPolicy(f.Key.Queue()))
		key, err := jobs.NewUniqueKey[Payload]("account-7")
		if err != nil {
			t.Fatal(err)
		}
		capture := func() jobs.Envelope {
			pending, err := definition.Capture(t.Context(), Payload{}, jobs.Options[Payload]{Unique: jobs.Unique[Payload]{Key: key, For: time.Hour, UntilProcessing: true}})
			if err != nil {
				t.Fatal(err)
			}
			return pending.Envelope()
		}
		if inserted, err := f.Backend.JobEnqueue(t.Context(), f.Key, capture()); err != nil || !inserted {
			t.Fatal(inserted, err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, capture()); !errors.Is(err, jobs.ErrNotUnique) {
			t.Fatal("waiting job did not hold its window", err)
		}
		reservation := reserveOne(t, f)
		if _, err := f.Backend.JobStart(t.Context(), f.Key, reservation.Ownership); err != nil {
			t.Fatal(err)
		}
		// Processing started: a new job with the same key may be queued once.
		if inserted, err := f.Backend.JobEnqueue(t.Context(), f.Key, capture()); err != nil || !inserted {
			t.Fatal("window not released at processing start", inserted, err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, capture()); !errors.Is(err, jobs.ErrNotUnique) {
			t.Fatal("the new job did not hold its own window", err)
		}
	})
	t.Run("stats-and-forget", func(t *testing.T) {
		f := makeFixture(t)
		stats, statsOK := f.Backend.(jobs.StatsBackend)
		forget, forgetOK := f.Backend.(jobs.ForgetBackend)
		if !statsOK || !forgetOK {
			t.Skip("backend does not implement queue operations")
		}
		policy := jobs.DefaultPolicy(f.Key.Queue())
		policy.Attempts = 1
		definition := jobs.Define[Payload]("stats.work", 1, policy)
		failed := enqueueOne(t, f, definition)
		reservation := reserveOne(t, f)
		if _, err := f.Backend.JobStart(t.Context(), f.Key, reservation.Ownership); err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobFinish(t.Context(), f.Key, reservation.Ownership, jobs.Result{State: jobs.Failed, Reason: jobs.HandlerFailed}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		live := enqueueOne(t, f, definition)
		delayed, err := definition.Capture(t.Context(), Payload{Text: "delayed"}, jobs.Options[Payload]{At: time.Now().Add(24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, delayed.Envelope()); err != nil {
			t.Fatal(err)
		}
		counts, err := stats.JobStats(t.Context(), f.Key)
		if err != nil || counts != (jobs.QueueStats{Waiting: 1, Delayed: 1, Failed: 1, Retained: 1}) {
			t.Fatalf("stats: %+v %v", counts, err)
		}
		if changed, err := forget.JobForget(t.Context(), f.Key, live.Target()); err != nil || changed {
			t.Fatal("live job forgotten", changed, err)
		}
		if changed, err := forget.JobForget(t.Context(), f.Key, failed.Target()); err != nil || !changed {
			t.Fatal("failed job not forgotten", changed, err)
		}
		if found, err := f.Backend.JobInspect(t.Context(), f.Key, failed.ID()); err != nil || found.IsSet() {
			t.Fatal("forgotten job still retained", err)
		}
		if inserted, err := f.Backend.JobEnqueue(t.Context(), f.Key, failed); err != nil || !inserted {
			t.Fatal("forgotten identity still deduplicated", inserted, err)
		}
	})
}

func enqueueOne(t *testing.T, f Fixture, definition jobs.Definition[Payload]) jobs.Envelope {
	t.Helper()
	pending, err := definition.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := f.Backend.JobEnqueue(t.Context(), f.Key, pending.Envelope()); err != nil || !inserted {
		t.Fatal(inserted, err)
	}
	return pending.Envelope()
}

func reserveOne(t *testing.T, f Fixture) jobs.Reservation {
	t.Helper()
	owner, err := lease.NewOwner()
	if err != nil {
		t.Fatal(err)
	}
	found, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reservation, ok := found.Get()
	if !ok {
		t.Fatal("expected a reservation")
	}
	return reservation
}

func inspectOne(t *testing.T, f Fixture, id jobs.ExecutionID) jobs.Record {
	t.Helper()
	found, err := f.Backend.JobInspect(t.Context(), f.Key, id)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := found.Get()
	if !ok {
		t.Fatal("record missing")
	}
	return record
}
