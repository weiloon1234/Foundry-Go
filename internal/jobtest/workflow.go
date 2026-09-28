package jobtest

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
)

func workflows(t *testing.T, makeFixture func(*testing.T) Fixture) {
	for _, kind := range []jobs.WorkflowKind{jobs.ChainKind, jobs.BatchKind} {
		t.Run(string(kind), func(t *testing.T) {
			f := makeFixture(t)
			ctx := t.Context()
			d := jobs.Define[Payload]("workflow.step", 1, jobs.DefaultPolicy(f.Key.Queue()))
			var steps []jobs.Step
			var envelopes []jobs.Envelope
			for i := range 3 {
				pending, err := d.Capture(ctx, Payload{Exact: uint64(i)}, jobs.Options[Payload]{})
				if err != nil {
					t.Fatal(err)
				}
				steps = append(steps, pending.Step())
				envelopes = append(envelopes, pending.Envelope())
			}
			var group jobs.Workflow
			var err error
			if kind == jobs.ChainKind {
				group, err = jobs.NewChain(steps...)
			} else {
				group, err = jobs.NewBatch(steps[:2]...)
				if err == nil {
					group, err = group.WithCompletion(steps[2])
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := group.Envelope().MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := jobs.DecodeWorkflow(encoded)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				ok, err := f.Backend.JobWorkflow(ctx, f.Key, restored)
				if err != nil || ok != (i == 0) {
					t.Fatal(ok, err)
				}
			}
			for i := range 3 {
				owner, err := lease.NewOwner()
				if err != nil {
					t.Fatal(err)
				}
				found, err := f.Backend.JobReserve(ctx, f.Key, owner, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				claim, ok := found.Get()
				if !ok {
					t.Fatalf("step %d not released", i)
				}
				if kind == jobs.ChainKind && claim.Envelope.ID() != envelopes[i].ID() {
					t.Fatal("chain ran out of order")
				}
				if kind == jobs.BatchKind && i < 2 && claim.Envelope.ID() == envelopes[2].ID() {
					t.Fatal("completion ran before all members")
				}
				if _, err := f.Backend.JobStart(ctx, f.Key, claim.Ownership); err != nil {
					t.Fatal(err)
				}
				if ok, err := f.Backend.JobFinish(ctx, f.Key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			page, err := f.Backend.JobList(ctx, f.Key, jobs.ListOptions{Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Records) != 2 || page.Next.IsZero() {
				t.Fatal("first workflow page is incomplete")
			}
			next, err := f.Backend.JobList(ctx, f.Key, jobs.ListOptions{Limit: 2, After: page.Next})
			if err != nil || len(next.Records) != 1 || !next.Next.IsZero() {
				t.Fatal("last workflow page is incorrect", err)
			}
			for _, record := range append(page.Records, next.Records...) {
				if record.State != jobs.Succeeded || record.Workflow != group.ID() {
					t.Fatal("workflow progress lost")
				}
			}
			for _, envelope := range envelopes {
				id := model.IDFromBytes[jobs.Execution](envelope.ID().Bytes())
				found, err := f.Backend.JobInspect(ctx, f.Key, id)
				if err != nil {
					t.Fatal(err)
				}
				record, ok := found.Get()
				if !ok || record.State != jobs.Succeeded {
					t.Fatal("completed member disappeared")
				}
			}
		})
	}
	t.Run("batch-partial-failure", func(t *testing.T) {
		f := makeFixture(t)
		d := jobs.Define[Payload]("batch.step", 1, jobs.DefaultPolicy(f.Key.Queue()))
		var steps []jobs.Step
		var completion jobs.Envelope
		for i := range 3 {
			pending, err := d.Capture(t.Context(), Payload{Exact: uint64(i)}, jobs.Options[Payload]{})
			if err != nil {
				t.Fatal(err)
			}
			steps = append(steps, pending.Step())
			completion = pending.Envelope()
		}
		group, err := jobs.NewBatch(steps[:2]...)
		if err != nil {
			t.Fatal(err)
		}
		group, err = group.WithCompletion(steps[2])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			owner, _ := lease.NewOwner()
			found, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			claim, ok := found.Get()
			if !ok {
				t.Fatal("batch stopped other work")
			}
			if _, err := f.Backend.JobStart(t.Context(), f.Key, claim.Ownership); err != nil {
				t.Fatal(err)
			}
			result := jobs.Result{State: jobs.Succeeded}
			if i == 0 {
				result = jobs.Result{State: jobs.Failed, Reason: jobs.HandlerFailed}
			}
			if _, err := f.Backend.JobFinish(t.Context(), f.Key, claim.Ownership, result); err != nil {
				t.Fatal(err)
			}
		}
		found, err := f.Backend.JobInspect(t.Context(), f.Key, completion.ID())
		if err != nil {
			t.Fatal(err)
		}
		record, ok := found.Get()
		if !ok || record.State != jobs.Cancelled {
			t.Fatal("failed batch ran completion")
		}
	})
	t.Run("chain-cancel-and-expiry", func(t *testing.T) {
		f := makeFixture(t)
		d := jobs.Define[Payload]("cancel.step", 1, jobs.DefaultPolicy(f.Key.Queue()))
		first, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		second, err := d.Capture(t.Context(), Payload{}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		group, err := jobs.NewChain(first.Step(), second.Step())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Backend.JobWorkflow(t.Context(), f.Key, group.Envelope()); err != nil {
			t.Fatal(err)
		}
		owner, _ := lease.NewOwner()
		found, err := f.Backend.JobReserve(t.Context(), f.Key, owner, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		claim, ok := found.Get()
		if !ok {
			t.Fatal("missing first step")
		}
		if _, err := f.Backend.JobStart(t.Context(), f.Key, claim.Ownership); err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobCancelWorkflow(t.Context(), f.Key, group.ID()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		current, err := f.Backend.JobInspect(t.Context(), f.Key, first.Envelope().ID())
		if err != nil {
			t.Fatal(err)
		}
		record, _ := current.Get()
		if record.State != jobs.Running || !record.CancellationRequested {
			t.Fatal("cancellation abandoned live member")
		}
		f.Expire(claim)
		page, err := f.Backend.JobList(t.Context(), f.Key, jobs.ListOptions{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 2 {
			t.Fatal("workflow cancellation lost history")
		}
		for _, r := range page.Records {
			if r.State != jobs.Cancelled {
				t.Fatal("workflow cancellation did not finish")
			}
		}
	})
}
