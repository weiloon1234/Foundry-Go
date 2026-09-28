package jobtest

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func failureBoundaries(t *testing.T, makeFixture func(*testing.T) Fixture) {
	t.Run("cancelled-completion-keeps-live-batch", func(t *testing.T) {
		f := makeFixture(t)
		d := jobs.Define[Payload]("cancel.batch", 1, jobs.DefaultPolicy(f.Key.Queue()))
		member, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		completion, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		group, err := jobs.NewBatch(member.Step())
		if err != nil {
			t.Fatal(err)
		}
		group, err = group.WithCompletion(completion.Step())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); err != nil {
			t.Fatal(err)
		}
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
			t.Fatal("batch member missing")
		}
		if _, err := f.Backend.JobStart(t.Context(), f.Key, claim.Ownership); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if changed, err := f.Backend.JobCancelWorkflow(t.Context(), f.Key, group.ID()); err != nil || !changed {
				t.Fatal("cancelled completion prematurely finished live batch", err)
			}
		}
		f.Expire(claim)
		page, err := f.Backend.JobList(t.Context(), f.Key, jobs.ListOptions{Limit: 10})
		if err != nil || len(page.Records) != 2 {
			t.Fatal("lost batch history", err)
		}
		for _, record := range page.Records {
			if record.State != jobs.Cancelled {
				t.Fatal("batch member did not finish cancellation")
			}
		}
		f.Advance(8 * 24 * time.Hour)
		page, err = f.Backend.JobList(t.Context(), f.Key, jobs.ListOptions{Limit: 10})
		if err != nil || len(page.Records) != 0 {
			t.Fatal("finished batch failed to retire", err)
		}
	})
	t.Run("retry-zero-delay-and-exhaustion", func(t *testing.T) {
		f := makeFixture(t)
		policy := jobs.DefaultPolicy(f.Key.Queue())
		policy.Attempts = 2
		d := jobs.Define[Payload]("retry", 1, policy)
		p, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, p.Envelope()); err != nil {
			t.Fatal(err)
		}
		for attempt := uint32(1); attempt <= 2; attempt++ {
			owner, _ := lease.NewOwner()
			found, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			r, ok := found.Get()
			if !ok {
				t.Fatal("retry not available")
			}
			if started, err := f.Backend.JobStart(t.Context(), f.Key, r.Ownership); err != nil || started != attempt {
				t.Fatal(started, err)
			}
			if ok, err := f.Backend.JobFinish(t.Context(), f.Key, r.Ownership, jobs.Result{State: jobs.Waiting, Reason: jobs.HandlerFailed}); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		found, err := f.Backend.JobInspect(t.Context(), f.Key, p.Envelope().ID())
		if err != nil {
			t.Fatal(err)
		}
		r, ok := found.Get()
		if !ok || r.State != jobs.Failed || r.Attempts != 2 {
			t.Fatal("attempt budget reset")
		}
	})
	t.Run("unique-expiry", func(t *testing.T) {
		f := makeFixture(t)
		d := jobs.Define[Payload]("unique.expiry", 1, jobs.DefaultPolicy(f.Key.Queue()))
		unique, err := jobs.NewUniqueKey[Payload]("same")
		if err != nil {
			t.Fatal(err)
		}
		options := jobs.Options[Payload]{Unique: jobs.Unique[Payload]{Key: unique, For: time.Second}}
		first, err := d.Capture(t.Context(), Payload{}, options)
		if err != nil {
			t.Fatal(err)
		}
		second, err := d.Capture(t.Context(), Payload{}, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, first.Envelope()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, second.Envelope()); !errors.Is(err, jobs.ErrNotUnique) {
			t.Fatal(err)
		}
		f.Advance(time.Second)
		if ok, err := f.Backend.JobEnqueue(t.Context(), f.Key, second.Envelope()); err != nil || !ok {
			t.Fatal("unique window did not expire", err)
		}
	})
	t.Run("atomic-group-conflict-and-terminal-retention", func(t *testing.T) {
		f := makeFixture(t)
		d := jobs.Define[Payload]("atomic", 1, jobs.DefaultPolicy(f.Key.Queue()))
		a, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobEnqueue(t.Context(), f.Key, b.Envelope()); err != nil {
			t.Fatal(err)
		}
		group, err := jobs.NewChain(a.Step(), b.Step())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if found, err := f.Backend.JobInspect(t.Context(), f.Key, a.Envelope().ID()); err != nil || found.IsSet() {
			t.Fatal("failed group partially inserted", err)
		}
		c, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		group, err = jobs.NewChain(a.Step(), c.Step())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobCancelWorkflow(t.Context(), f.Key, group.ID()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		f.Advance(8 * 24 * time.Hour)
		if found, err := f.Backend.JobInspect(t.Context(), f.Key, a.Envelope().ID()); err != nil || found.IsSet() {
			t.Fatal("terminal group retained past window", err)
		}
		if inserted, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); err != nil || !inserted {
			t.Fatal("retired workflow cannot be reaccepted", err)
		}
	})
}
