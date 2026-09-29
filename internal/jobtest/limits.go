package jobtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// workflowLimits runs the largest accepted batch, MaxWorkflowSteps members
// plus completion, catch and finally, to its end: every callback position is
// within the authority's bounds, and one step more is rejected before it.
func workflowLimits(t *testing.T, makeFixture func(*testing.T) Fixture) {
	f := makeFixture(t)
	ctx := t.Context()
	d := jobs.Define[Payload]("workflow.maximum", 1, jobs.DefaultPolicy(f.Key.Queue()))
	capture := func(text string) jobs.Step {
		t.Helper()
		pending, err := d.Capture(ctx, Payload{Text: text}, jobs.Options[Payload]{})
		if err != nil {
			t.Fatal(err)
		}
		return pending.Step()
	}
	steps := make([]jobs.Step, jobs.MaxWorkflowSteps+1)
	for i := range steps {
		steps[i] = capture(fmt.Sprint("step-", i))
	}
	if _, err := jobs.NewBatch(steps...); err == nil {
		t.Fatal("a batch above MaxWorkflowSteps was accepted")
	}
	group, err := jobs.NewBatch(steps[:jobs.MaxWorkflowSteps]...)
	if err == nil {
		group, err = group.WithCompletion(capture("completion"))
	}
	if err == nil {
		group, err = group.WithCatch(capture("catch"))
	}
	if err == nil {
		group, err = group.WithFinally(capture("finally"))
	}
	if err != nil {
		t.Fatal(err)
	}
	if members := len(group.Envelope().Members()); members != jobs.MaxWorkflowMembers {
		t.Fatal("unexpected member count", members)
	}
	if ok, err := f.Backend.JobWorkflow(ctx, f.Key, group.Envelope()); err != nil || !ok {
		t.Fatal("maximum workflow rejected", ok, err)
	}
	ran := map[jobs.ExecutionID]bool{}
	for {
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
			break
		}
		if _, err := f.Backend.JobStart(ctx, f.Key, claim.Ownership); err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Backend.JobFinish(ctx, f.Key, claim.Ownership, jobs.Result{State: jobs.Succeeded}); err != nil || !ok {
			t.Fatal(ok, err)
		}
		ran[claim.Envelope.ID()] = true
	}
	envelope := group.Envelope()
	completion, _ := envelope.Completion().Get()
	catch, _ := envelope.Catch().Get()
	finally, _ := envelope.Finally().Get()
	if len(ran) != jobs.MaxWorkflowSteps+2 || !ran[completion.ID()] || !ran[finally.ID()] || ran[catch.ID()] {
		t.Fatal("maximum workflow did not run its steps, completion and finally", len(ran))
	}
	found, err := f.Backend.JobInspect(ctx, f.Key, catch.ID())
	record, present := found.Get()
	if err != nil || !present || record.State != jobs.Cancelled || record.Position != uint32(jobs.MaxWorkflowMembers-2) {
		t.Fatal("untriggered catch not cancelled at its position", err)
	}
	statuses, ok := f.Backend.(jobs.WorkflowStatusBackend)
	if !ok {
		t.Fatal("backend does not report workflow status")
	}
	status, err := statuses.JobWorkflowStatus(ctx, f.Key, group.ID())
	value, present := status.Get()
	// Status counts the steps and the completion job; callbacks are excluded.
	if err != nil || !present || !value.Finished() || value.Total != jobs.MaxWorkflowSteps+1 || value.Succeeded != value.Total {
		t.Fatalf("maximum workflow status: %+v %v", value, err)
	}
}
