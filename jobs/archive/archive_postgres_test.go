package archive_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type Export struct {
	Report string `json:"report"`
}

func openArchive(t *testing.T) *archive.Store {
	t.Helper()
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range archive.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	store, err := archive.New(db, schema, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestArchiveRecordsTerminalFailuresAndRedispatchesThem(t *testing.T) {
	store := openArchive(t)
	var fail atomic.Bool
	fail.Store(true)
	var runs atomic.Int32
	policy := jobs.DefaultPolicy("reports")
	policy.Attempts = 1
	definition := jobs.Define[Export]("reports.export", 1, policy)
	declaration, err := definition.Declare(func(_ context.Context, e Export) error {
		runs.Add(1)
		if fail.Load() {
			return jobs.Permanent(errors.New("private failure"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	namespace := keyspace.Namespace{Application: "archive", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(namespace, "reports")
	config.PollInterval = time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config, jobs.WithFailureSink(store))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = worker.Stop(ctx)
		<-done
	})
	receipt, err := definition.Dispatch(t.Context(), dispatcher, Export{Report: "monthly"}, jobs.Options[Export]{})
	if err != nil {
		t.Fatal(err)
	}
	var page archive.Page
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if page, err = store.List(t.Context(), archive.ListOptions{Name: "reports.export"}); err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(page.Entries) != 1 {
		t.Fatal("terminal failure was not archived")
	}
	entry := page.Entries[0]
	if entry.Execution.String() != receipt.ID.String() || entry.Reason != jobs.HandlerFailed || entry.Attempts != 1 || entry.Exceptions != 1 || entry.Queue != "reports" {
		t.Fatalf("archived entry: %+v", entry)
	}
	fail.Store(false)
	execution, err := store.Retry(t.Context(), dispatcher, entry.ID)
	if err != nil || execution.String() == receipt.ID.String() {
		t.Fatal("archived failure was not re-dispatched as a new job", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && runs.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if runs.Load() != 2 {
		t.Fatal("re-dispatched job did not run")
	}
	if page, err := store.List(t.Context(), archive.ListOptions{}); err != nil || page.Entries[0].RetriedAt.IsZero() {
		t.Fatal("retry was not recorded", err)
	}
	if deleted, err := store.Prune(t.Context(), time.Now().Add(-time.Hour), 10); err != nil || deleted != 0 {
		t.Fatal("prune removed a recent entry", deleted, err)
	}
	if deleted, err := store.Prune(t.Context(), time.Now().Add(time.Hour), 10); err != nil || deleted != 1 {
		t.Fatal("prune", deleted, err)
	}
	// Recording the same failure cycle twice keeps one entry.
	failed := jobs.FailedJob{Queue: "reports", Envelope: mustEnvelope(t, definition), Reason: jobs.HandlerFailed, Attempts: 1}
	for range 2 {
		if err := store.RecordFailure(t.Context(), failed); err != nil {
			t.Fatal(err)
		}
	}
	if page, err := store.List(t.Context(), archive.ListOptions{}); err != nil || len(page.Entries) != 1 {
		t.Fatal("duplicate failure record", err)
	}
}

func mustEnvelope(t *testing.T, definition jobs.Definition[Export]) jobs.Envelope {
	t.Helper()
	pending, err := definition.Capture(t.Context(), Export{Report: "weekly"}, jobs.Options[Export]{})
	if err != nil {
		t.Fatal(err)
	}
	return pending.Envelope()
}

// Scaled is dense JSON: PostgreSQL jsonb re-renders it with a space after
// every separator, so a payload near the bound would outgrow it.
type Scaled struct {
	Values []int  `json:"values"`
	Pad    string `json:"pad"`
}

func TestArchivePagesByPositionAndKeepsOriginalEnvelopeBytes(t *testing.T) {
	store := openArchive(t)
	policy := jobs.DefaultPolicy("reports")
	export := jobs.Define[Export]("reports.page", 1, policy)
	scaled := jobs.Define[Scaled]("reports.scaled", 1, policy)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	// Two entries share one instant; paging must list each exactly once.
	for _, at := range []time.Duration{0, time.Second, time.Second, 2 * time.Second} {
		failed := jobs.FailedJob{Queue: "reports", Envelope: mustCapture(t, export, Export{Report: "page"}), Reason: jobs.HandlerFailed, Attempts: 1, FailedAt: base.Add(at)}
		if err := store.RecordFailure(t.Context(), failed); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[archive.ID]bool{}
	var previous time.Time
	options := archive.ListOptions{Name: "reports.page", Limit: 1}
	for pages := 0; ; pages++ {
		if pages > 4 {
			t.Fatal("listing did not terminate")
		}
		page, err := store.List(t.Context(), options)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			if seen[entry.ID] || !previous.IsZero() && entry.FailedAt.After(previous) {
				t.Fatal("entry repeated or out of order", entry.ID)
			}
			seen[entry.ID], previous = true, entry.FailedAt
		}
		if page.Next.IsZero() {
			break
		}
		// The cursor survives a text round trip, as the command prints it.
		options.After, err = archive.ParseCursor(page.Next.String())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 4 {
		t.Fatal("paging skipped entries sharing an instant", len(seen))
	}
	// Pruning the last listed entry does not restart the listing.
	page, err := store.List(t.Context(), archive.ListOptions{Name: "reports.page", Limit: 2})
	if err != nil || len(page.Entries) != 2 || page.Next.IsZero() {
		t.Fatal("first page", err)
	}
	if deleted, err := store.Prune(t.Context(), base.Add(1500*time.Millisecond), 10); err != nil || deleted != 3 {
		t.Fatal("prune", deleted, err)
	}
	if rest, err := store.List(t.Context(), archive.ListOptions{Name: "reports.page", Limit: 2, After: page.Next}); err != nil || len(rest.Entries) != 0 {
		t.Fatal("continuing after a pruned entry restarted the listing", len(rest.Entries), err)
	}
	if _, err := store.List(t.Context(), archive.ListOptions{Limit: 2, After: page.Next}); err == nil {
		t.Fatal("cursor accepted by a listing with a different filter")
	}
	if _, err := archive.ParseCursor("not-a-cursor"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
	// The stored envelope is the original bytes: jsonb's rendering of this
	// near-limit payload would exceed jobs.MaxPayloadBytes on retry.
	values := make([]int, 9000)
	payload := Scaled{Values: values, Pad: strings.Repeat("x", jobs.MaxPayloadBytes-2*len(values)-256)}
	failed := jobs.FailedJob{Queue: "reports", Envelope: mustCapture(t, scaled, payload), Reason: jobs.HandlerFailed, Attempts: 1}
	if err := store.RecordFailure(t.Context(), failed); err != nil {
		t.Fatal("envelope within the transport bound rejected", err)
	}
	listed, err := store.List(t.Context(), archive.ListOptions{Name: "reports.scaled"})
	if err != nil || len(listed.Entries) != 1 {
		t.Fatal("scaled failure not archived", err)
	}
	declaration, err := scaled.Declare(func(context.Context, Scaled) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "archive", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Retry(t.Context(), dispatcher, listed.Entries[0].ID); err != nil {
		t.Fatal("archived envelope did not round-trip for retry", err)
	}
}

func mustCapture[P any](t *testing.T, definition jobs.Definition[P], payload P) jobs.Envelope {
	t.Helper()
	pending, err := definition.Capture(t.Context(), payload, jobs.Options[P]{})
	if err != nil {
		t.Fatal(err)
	}
	return pending.Envelope()
}
