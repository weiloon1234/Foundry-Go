package jobtest

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// callbacks covers workflow catch/finally jobs and the status API.
func callbacks(t *testing.T, makeFixture func(*testing.T) Fixture) {
	for _, fail := range []bool{true, false} {
		name := "all-succeed"
		if fail {
			name = "member-fails"
		}
		t.Run(name, func(t *testing.T) {
			f := makeFixture(t)
			ctx := t.Context()
			d := jobs.Define[Payload]("workflow.callback", 1, jobs.DefaultPolicy(f.Key.Queue()))
			capture := func(text string) jobs.Pending[Payload] {
				pending, err := d.Capture(ctx, Payload{Text: text}, jobs.Options[Payload]{})
				if err != nil {
					t.Fatal(err)
				}
				return pending
			}
			first, second, catch, finally := capture("first"), capture("second"), capture("catch"), capture("finally")
			group, err := jobs.NewBatch(first.Step(), second.Step())
			if err == nil {
				group, err = group.WithCatch(catch.Step())
			}
			if err == nil {
				group, err = group.WithFinally(finally.Step())
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := group.Envelope().MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := jobs.DecodeWorkflow(encoded)
			if err != nil || !restored.Catch().IsSet() || !restored.Finally().IsSet() {
				t.Fatal("callbacks did not round-trip", err)
			}
			if ok, err := f.Backend.JobWorkflow(ctx, f.Key, restored); err != nil || !ok {
				t.Fatal(ok, err)
			}
			statuses, ok := f.Backend.(jobs.WorkflowStatusBackend)
			if !ok {
				t.Fatal("backend does not report workflow status")
			}
			status := func() jobs.WorkflowStatus {
				t.Helper()
				found, err := statuses.JobWorkflowStatus(ctx, f.Key, group.ID())
				value, present := found.Get()
				if err != nil || !present {
					t.Fatal("workflow status missing", err)
				}
				return value
			}
			if s := status(); s.Total != 2 || s.Pending != 2 || s.Finished() {
				t.Fatalf("initial status: %+v", s)
			}
			run := func(state jobs.State) jobs.ExecutionID {
				t.Helper()
				owner, err := lease.NewOwner()
				if err != nil {
					t.Fatal(err)
				}
				found, err := f.Backend.JobReserve(ctx, f.Key, owner, time.Minute)
				claim, ok := found.Get()
				if err != nil || !ok {
					t.Fatal("expected a released job", err)
				}
				if _, err := f.Backend.JobStart(ctx, f.Key, claim.Ownership); err != nil {
					t.Fatal(err)
				}
				result := jobs.Result{State: state}
				if state == jobs.Failed {
					result.Reason = jobs.HandlerFailed
				}
				if ok, err := f.Backend.JobFinish(ctx, f.Key, claim.Ownership, result); err != nil || !ok {
					t.Fatal(ok, err)
				}
				return claim.Envelope.ID()
			}
			ran := map[jobs.ExecutionID]bool{}
			ran[run(jobs.Succeeded)] = true
			outcome := jobs.Succeeded
			if fail {
				outcome = jobs.Failed
			}
			ran[run(outcome)] = true
			if !ran[first.Envelope().ID()] || !ran[second.Envelope().ID()] {
				t.Fatal("a callback ran before the members settled")
			}
			s := status()
			if s.Processed != 2 || s.Failed != fail || s.Finished() || fail && s.FailedJobs != 1 {
				t.Fatalf("settled status: %+v", s)
			}
			// Callbacks now run: catch only after a failure, finally always.
			expected := map[jobs.ExecutionID]bool{finally.Envelope().ID(): true}
			if fail {
				expected[catch.Envelope().ID()] = true
			}
			for range expected {
				id := run(jobs.Succeeded)
				if !expected[id] {
					t.Fatal("unexpected callback ran")
				}
			}
			owner, err := lease.NewOwner()
			if err != nil {
				t.Fatal(err)
			}
			if found, err := f.Backend.JobReserve(ctx, f.Key, owner, time.Minute); err != nil || found.IsSet() {
				t.Fatal("an untriggered callback was released", err)
			}
			if !status().Finished() {
				t.Fatal("workflow did not finish after its callbacks")
			}
			if !fail {
				found, err := f.Backend.JobInspect(ctx, f.Key, catch.Envelope().ID())
				record, _ := found.Get()
				if err != nil || record.State != jobs.Cancelled || record.History[len(record.History)-1].Reason != jobs.NotTriggered {
					t.Fatal("untriggered catch was not cancelled", err)
				}
			}
		})
	}
}
