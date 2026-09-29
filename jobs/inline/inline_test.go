package inline_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/inline"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type Welcome struct {
	User string `json:"user"`
}

type Audit struct {
	Note string `json:"note"`
}

func TestSyncDriverRunsJobsInlineWithWorkerSemantics(t *testing.T) {
	clock := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	config := memory.DefaultConfig()
	config.Clock = clock
	backend, err := inline.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	policy := jobs.DefaultPolicy("default")
	policy.Backoff, policy.Jitter = []time.Duration{time.Minute}, 0
	welcome := jobs.Define[Welcome]("welcome", 1, policy)
	audit := jobs.Define[Audit]("audit", 1, jobs.DefaultPolicy("audit"))
	var dispatcher *jobs.Dispatcher
	var order []string
	var fail atomic.Bool
	welcomeDeclaration, err := welcome.DeclareWith(func(ctx context.Context, w Welcome) error {
		order = append(order, "welcome:"+w.User)
		// A dispatch from a running handler runs after this handler returns.
		if _, err := audit.Dispatch(ctx, dispatcher, Audit{Note: w.User}, jobs.Options[Audit]{}); err != nil {
			return err
		}
		order = append(order, "welcome-done")
		if fail.Load() {
			return errors.New("provider down")
		}
		return nil
	}, jobs.HandlerOptions[Welcome]{Middleware: []jobs.Middleware[Welcome]{{Before: func(context.Context, Welcome) error { order = append(order, "before"); return nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	auditDeclaration, err := audit.Declare(func(_ context.Context, a Audit) error { order = append(order, "audit:"+a.Note); return nil })
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(welcomeDeclaration, auditDeclaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err = jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "inline", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := welcome.Dispatch(t.Context(), dispatcher, Welcome{User: "ada"}, jobs.Options[Welcome]{})
	if err != nil || !receipt.Inserted {
		t.Fatal(receipt, err)
	}
	want := []string{"before", "welcome:ada", "welcome-done", "audit:ada"}
	if len(order) != len(want) {
		t.Fatal("inline order", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatal("inline order", order)
		}
	}
	record, err := welcome.Inspect(t.Context(), dispatcher, receipt.ID, "")
	if stored, _ := record.Get(); err != nil || stored.State != jobs.Succeeded {
		t.Fatal("inline job was not finalized", err)
	}
	// A failure is recorded on the job and retried once due, not returned.
	fail.Store(true)
	receipt, err = welcome.Dispatch(t.Context(), dispatcher, Welcome{User: "bob"}, jobs.Options[Welcome]{})
	if err != nil {
		t.Fatal("handler failure was returned from Dispatch", err)
	}
	record, _ = welcome.Inspect(t.Context(), dispatcher, receipt.ID, "")
	if stored, _ := record.Get(); stored.State != jobs.Waiting || stored.Attempts != 1 {
		t.Fatal("failed inline attempt not scheduled for retry", stored.State, stored.Attempts)
	}
	fail.Store(false)
	if processed, err := dispatcher.RunPending(t.Context(), "default"); err != nil || processed != 0 {
		t.Fatal("retry ran before it was due", processed, err)
	}
	clock.Advance(time.Minute)
	// The due retry runs, followed by the audit job its handler dispatched.
	if processed, err := dispatcher.RunPending(t.Context(), "default"); err != nil || processed != 2 {
		t.Fatal("due retry did not run", processed, err)
	}
	record, _ = welcome.Inspect(t.Context(), dispatcher, receipt.ID, "")
	if stored, _ := record.Get(); stored.State != jobs.Succeeded || stored.Attempts != 2 {
		t.Fatal("retry outcome", stored.State, stored.Attempts)
	}
}

func TestRunPendingRequiresAnInlineBackend(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	definition := jobs.Define[Welcome]("welcome", 1, jobs.DefaultPolicy("default"))
	declaration, err := definition.Declare(func(context.Context, Welcome) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "inline", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.RunPending(t.Context(), "default"); !errors.Is(err, fault.Invalid) {
		t.Fatal("queued backend executed inline", err)
	}
}
