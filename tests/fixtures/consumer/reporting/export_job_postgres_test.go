package reporting_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type exportDestination func(context.Context, jobs.ID[reporting.ExportMembers], *datatable.Artifact) error

func (f exportDestination) Store(ctx context.Context, id jobs.ID[reporting.ExportMembers], artifact *datatable.Artifact) error {
	return f(ctx, id, artifact)
}

type exportQueue struct {
	dispatcher *jobs.Dispatcher
	worker     *jobs.Worker
	clock      *testkit.Clock
}

func newExportQueue(t *testing.T, fixture *fixture, destination reporting.Destination) *exportQueue {
	t.Helper()
	clock := testkit.NewClock(time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC))
	config := memory.DefaultConfig()
	config.Clock = clock
	backend, err := memory.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	declaration, err := reporting.ExportDeclaration(fixture.manager, fixture.provider, destination)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	namespace := keyspace.Namespace{Application: "reporting", Environment: "test"}
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	workerConfig := jobs.DefaultWorkerConfig(namespace, "exports")
	workerConfig.Concurrency, workerConfig.PollInterval = 1, time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, workerConfig)
	if err != nil {
		t.Fatal(err)
	}
	return &exportQueue{dispatcher: dispatcher, worker: worker, clock: clock}
}

func (q *exportQueue) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- q.worker.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := q.worker.Stop(ctx); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("export worker did not stop")
		}
	})
}

func (q *exportQueue) dispatch(t *testing.T, payload reporting.ExportMembers) jobs.ID[reporting.ExportMembers] {
	t.Helper()
	receipt, err := reporting.ExportMembersJob.Dispatch(t.Context(), q.dispatcher, payload, jobs.Options[reporting.ExportMembers]{})
	if err != nil {
		t.Fatal(err)
	}
	return receipt.ID
}

func (q *exportQueue) await(t *testing.T, id jobs.ID[reporting.ExportMembers], predicate func(jobs.Record) bool) jobs.Record {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		found, err := reporting.ExportMembersJob.Inspect(t.Context(), q.dispatcher, id, "")
		if err != nil {
			t.Fatal(err)
		}
		if record, ok := found.Get(); ok {
			if predicate(record) {
				return record
			}
			// Advance only a waiting job's future retry time. Never move the
			// authority clock while an attempt holds a live lease.
			if record.State == jobs.Waiting && record.AvailableAt.After(q.clock.Now()) {
				q.clock.Set(record.AvailableAt.Add(time.Millisecond))
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("export job did not reach the expected state")
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-tick.C:
		}
	}
}

func exportPayload(f *fixture) reporting.ExportMembers {
	return reporting.ExportMembers{OperatorID: f.operator, Options: datatable.ExportOptions{
		Format: datatable.CSV, Name: "members.csv",
		Presentation: datatable.Presentation{Locale: "ms", TimeZone: "Asia/Kuala_Lumpur"},
	}}
}

func TestQueuedExportReloadsCurrentPermissionsBeforeEveryAttempt(t *testing.T) {
	f := openFixture(t)
	var calls atomic.Int32
	queue := newExportQueue(t, f, exportDestination(func(_ context.Context, _ jobs.ID[reporting.ExportMembers], artifact *datatable.Artifact) error {
		calls.Add(1)
		_, err := io.Copy(io.Discard, artifact)
		return err
	}))
	id := queue.dispatch(t, exportPayload(f))
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_operators SET can_export=false`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	queue.start(t)
	record := queue.await(t, id, func(record jobs.Record) bool { return record.State.Terminal() })
	if record.State != jobs.Failed || record.Attempts != reporting.ExportMembersJob.Policy().Attempts || calls.Load() != 0 {
		t.Fatal("queued export trusted captured authority", record.State, record.Attempts, calls.Load())
	}
	assertNoExportFiles(t, f)
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_operators SET can_export=true`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	id = queue.dispatch(t, exportPayload(f))
	record = queue.await(t, id, func(record jobs.Record) bool { return record.State.Terminal() })
	if record.State != jobs.Succeeded || calls.Load() != 1 {
		t.Fatal("fresh permission was not used", record.State, calls.Load())
	}
	assertNoExportFiles(t, f)
}

func TestQueuedExportRedeliveryUsesStableStorageIdentityAndClosesArtifacts(t *testing.T) {
	f := openFixture(t)
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("report-exports", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	var calls atomic.Int32
	artifacts := make(chan *datatable.Artifact, 2)
	queue := newExportQueue(t, f, exportDestination(func(ctx context.Context, id jobs.ID[reporting.ExportMembers], artifact *datatable.Artifact) error {
		artifacts <- artifact
		attempt := calls.Add(1)
		key, err := storage.ParseKey("exports/" + id.String() + ".csv")
		if err != nil {
			return err
		}
		checksum, err := storage.ParseSHA256(artifact.SHA256())
		if err != nil {
			return err
		}
		_, err = disk.Put(ctx, key, artifact, storage.PutOptions{ContentType: storage.MediaType(artifact.MediaType()), Size: value.Set(artifact.Size()), Checksum: value.Set(checksum), Condition: storage.IfAbsent()})
		if errors.Is(err, storage.PreconditionFailed) {
			// This destination owns the stable execution key. A retry retains
			// the first complete object instead of overwriting its snapshot.
			_, err = disk.Stat(ctx, key, storage.ReadOptions{})
		}
		if err != nil {
			return err
		}
		if attempt == 1 {
			return errors.New("delivery acknowledgement interrupted after durable storage")
		}
		return nil
	}))
	id := queue.dispatch(t, exportPayload(f))
	queue.start(t)
	queue.await(t, id, func(record jobs.Record) bool { return record.State == jobs.Waiting && record.Attempts == 1 })
	first := <-artifacts
	if _, err := first.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("failed delivery retained its artifact", err)
	}
	assertNoExportFiles(t, f)
	// A second export may observe new source values; storage must still retain
	// the first complete delivery for this same execution identity.
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_members SET name='Changed' WHERE id='0193fd8c-2075-7000-8000-000000000011'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record := queue.await(t, id, func(record jobs.Record) bool { return record.State.Terminal() })
	if record.State != jobs.Succeeded || record.Attempts != 2 || calls.Load() != 2 {
		t.Fatal("redelivery did not finish", record.State, record.Attempts, calls.Load())
	}
	second := <-artifacts
	if _, err := second.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("successful delivery retained its artifact", err)
	}
	assertNoExportFiles(t, f)
	key, err := storage.ParseKey("exports/" + id.String() + ".csv")
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := disk.ReadBytes(t.Context(), key, 1<<20, storage.ReadOptions{})
	if err != nil || !strings.Contains(string(body), "ms:reports.") || !strings.Contains(string(body), "9007199254740993.125") || strings.Contains(string(body), "Changed") || strings.Contains(string(body), "Foreign") || strings.Contains(string(body), "Deleted") {
		t.Fatal("stored snapshot, locale or tenant scope changed", err)
	}
}

func TestQueuedExportShutdownWaitsForDeliveryAndReleasesFile(t *testing.T) {
	f := openFixture(t)
	entered := make(chan *datatable.Artifact, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	queue := newExportQueue(t, f, exportDestination(func(ctx context.Context, _ jobs.ID[reporting.ExportMembers], artifact *datatable.Artifact) error {
		entered <- artifact
		<-release
		return ctx.Err()
	}))
	queue.dispatch(t, exportPayload(f))
	queue.start(t)
	// Unblock before worker cleanup even when an assertion fails.
	t.Cleanup(finish)
	var artifact *datatable.Artifact
	select {
	case artifact = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not start")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := queue.worker.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("worker abandoned an active delivery", err)
	}
	select {
	case <-queue.worker.Done():
		t.Fatal("worker reported done before actual delivery exit")
	default:
	}
	finish()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := queue.worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := artifact.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("stopped delivery retained its artifact", err)
	}
	assertNoExportFiles(t, f)
}
