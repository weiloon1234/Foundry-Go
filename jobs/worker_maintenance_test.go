package jobs_test

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Each wake subscription marks a new loop cycle. Permits let the test release
// every loop into the same maintenance pause without timing assumptions.
type maintenanceWorkerBackend struct {
	jobs.Backend
	cycles   chan chan struct{}
	release  chan struct{}
	reserves atomic.Int32
}

func (b *maintenanceWorkerBackend) JobWakeup(jobs.Key) <-chan struct{} {
	permit := make(chan struct{})
	select {
	case b.cycles <- permit:
	case <-b.release:
	}
	select {
	case <-permit:
	case <-b.release:
	}
	return nil
}

func (b *maintenanceWorkerBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	b.reserves.Add(1)
	return b.Backend.JobReserve(ctx, key, owner, ttl)
}

type maintenanceWorkerLogs struct {
	mu      sync.Mutex
	records []slog.Record
	emitted chan slog.Record
}

func (*maintenanceWorkerLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *maintenanceWorkerLogs) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, record.Clone())
	h.mu.Unlock()
	h.emitted <- record.Clone()
	return nil
}
func (h *maintenanceWorkerLogs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *maintenanceWorkerLogs) WithGroup(string) slog.Handler      { return h }

func TestWorkerMaintenanceReservationLogs(t *testing.T) {
	for _, test := range []struct {
		name        string
		logged      bool
		drainWorker bool
	}{
		{name: "gate-drain", logged: true},
		{name: "worker-drain", logged: true, drainWorker: true},
		{name: "no-logger"},
	} {
		t.Run(test.name, func(t *testing.T) {
			logged := test.logged
			const concurrency = 3
			ran := make(chan struct{}, 1)
			handler := func(context.Context, payload) error { ran <- struct{}{}; return nil }
			f := newWorkerFixture(t, jobs.DefaultPolicy("default"), handler)
			f.enqueue(t)
			declaration, err := f.definition.Declare(handler)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := jobs.NewRegistry(declaration)
			if err != nil {
				t.Fatal(err)
			}
			backend := &maintenanceWorkerBackend{Backend: f.backend, cycles: make(chan chan struct{}, concurrency), release: make(chan struct{})}
			logs := &maintenanceWorkerLogs{emitted: make(chan slog.Record, 16)}
			config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
			config.Concurrency, config.PollInterval, config.FailureLog = concurrency, time.Millisecond, false
			var options []jobs.WorkerOption
			if logged {
				options = append(options, jobs.WithWorkerLogger(slog.New(logs)))
			}
			worker, err := jobs.NewWorker(backend, registry, config, options...)
			if err != nil {
				t.Fatal(err)
			}
			var gate maintenance.Gate
			if err := gate.Set(true); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- worker.Run(maintenance.WithContext(t.Context(), &gate)) }()
			t.Cleanup(func() {
				close(backend.release)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := worker.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			var permits []chan struct{}
			waitCycle := func() {
				t.Helper()
				for range concurrency {
					select {
					case permit := <-backend.cycles:
						permits = append(permits, permit)
					case <-time.After(3 * time.Second):
						t.Fatal("loop did not reach next cycle")
					}
				}
			}
			releaseCycle := func() {
				for _, permit := range permits {
					close(permit)
				}
				permits = nil
			}
			waitLog := func(message string, level slog.Level) {
				t.Helper()
				if !logged {
					return
				}
				select {
				case record := <-logs.emitted:
					if record.Message != message || record.Level != level {
						t.Fatalf("unexpected log: %s %s", record.Level, record.Message)
					}
					attrs := 0
					record.Attrs(func(attr slog.Attr) bool {
						attrs++
						if attr.Key != "queues" || attr.Value.Kind() != slog.KindInt64 || attr.Value.Int64() != 1 {
							t.Error("unexpected maintenance attribute", attr)
						}
						return true
					})
					if attrs != 1 {
						t.Fatal("expected only queue count", attrs)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("missing log", message)
				}
			}
			waitCycle()
			releaseCycle()
			waitLog("job reservations held by maintenance", slog.LevelWarn)
			if backend.reserves.Load() != 0 {
				t.Fatal("reserved while maintenance paused")
			}
			if err := gate.Set(false); err != nil {
				t.Fatal(err)
			}
			waitLog("job reservations resumed after maintenance", slog.LevelInfo)
			select {
			case <-ran:
			case <-time.After(3 * time.Second):
				t.Fatal("resumed worker did not run queued job")
			}
			waitCycle()
			if err := gate.Set(true); err != nil {
				t.Fatal(err)
			}
			before := backend.reserves.Load()
			releaseCycle()
			waitLog("job reservations held by maintenance", slog.LevelWarn)
			if test.drainWorker {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				if err := worker.Drain(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				gate.Drain()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("maintenance drain did not stop worker")
			}
			if backend.reserves.Load() != before {
				t.Fatal("reserved during second pause or drain")
			}
			logs.mu.Lock()
			defer logs.mu.Unlock()
			want := 0
			if logged {
				want = 3
			}
			if len(logs.records) != want {
				t.Fatalf("got %d logs, want %d", len(logs.records), want)
			}
		})
	}
}
