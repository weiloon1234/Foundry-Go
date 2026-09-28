package schedule_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

type report struct {
	At time.Time `json:"at"`
}

func TestJobTargetRetainsOccurrenceIdentityAndAttribution(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	definition := jobs.Define[report]("report", 1, jobs.DefaultPolicy("reports"))
	declared, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declared)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "schedule", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	target, err := schedule.JobTarget(dispatcher, definition, func(_ context.Context, invocation schedule.Invocation) (report, error) {
		return report{At: invocation.IntendedAt}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, every(t, "reports.build", func(ctx context.Context, invocation schedule.Invocation) error {
		if err := target(ctx, invocation); err != nil {
			return err
		}
		return target(ctx, invocation) // deliberate duplicate of one occurrence
	}))
	f.start(t)
	f.advance(time.Minute)
	waitFor(t, func() bool { s := f.scheduler.Snapshot(); return len(s.History) == 1 && s.Active == 0 })
	history := f.scheduler.Snapshot().History[0]
	if history.State != schedule.Succeeded {
		t.Fatal(history.Reason)
	}
	page, err := dispatcher.List(t.Context(), "reports", jobs.ListOptions{Limit: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatal("occurrence was not deduplicated", err)
	}
	if page.Records[0].Envelope.ID().Bytes() != history.Invocation.Occurrence.Bytes() || page.Records[0].Envelope.Origin().System() != "reports.build" {
		t.Fatal("scheduled job lost stable identity or attribution")
	}
}
