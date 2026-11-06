package telemetry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
)

func TestStorageConcurrentWritersAndPrunerRespectGlobalCapacity(t *testing.T) {
	cc := collectorConfig()
	cc.MaxSeries, cc.MaxEvents = 128, 128
	config := capacityConfig()
	first, db, schema, firstClock := postgresStorage(t, cc, config)
	secondCollector, secondClock := newCollector(t, cc)
	second, err := newStorage(secondCollector, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	pruneCollector, _ := newCollector(t, cc)
	pruner, err := newStorage(pruneCollector, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		now, err := serverTime(t.Context(), tx)
		if err != nil {
			return err
		}
		firstClock.set(now.UTC())
		secondClock.set(now.UTC())
		return nil
	})
	for writer, s := range []*storage{first, second} {
		for i := range 60 {
			r := backendReport()
			r.Entry.Route.Name = fmt.Sprintf("concurrent.%d.%03d", writer, i)
			if err := s.collector.Report(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			if i < 35 {
				e := request()
				e.Route = http.RouteID(r.Entry.Route.Name)
				s.collector.ObserveRequest(t.Context(), e)
			}
		}
		capture(t, s.collector)
	}
	start, finished := make(chan struct{}), make(chan error, 3)
	for _, s := range []*storage{first, second} {
		go func() { <-start; finished <- s.flush(t.Context(), nil) }()
	}
	go func() { <-start; _, err := pruner.prune(t.Context()); finished <- err }()
	close(start)
	for range 3 {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 2, receipts: 2, metrics: 100, issues: 100, events: 100}) {
		t.Fatalf("concurrent capacity admission: %+v", got)
	}
	if dropped := first.collector.dropped + second.collector.dropped; dropped != 60 {
		t.Fatalf("concurrent storage loss = %d, want 40 bucket snapshots and 20 issue observations", dropped)
	}
}

func TestStorageRechecksExpiryAfterLockAndLeasesCaptureHead(t *testing.T) {
	s, db, schema, clock := postgresStorage(t, collectorConfig(), engineConfig())
	c := s.collector
	c.Report(t.Context(), backendReport())
	first := capture(t, c)
	locked, release, holder := holdTelemetryLock(t, db, schema)
	<-locked
	entered := make(chan struct{})
	transact := s.transact
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return transact(ctx, func(tx *database.Tx) error {
			close(entered)
			return fn(tx)
		}, options...)
	}
	done := make(chan error, 1)
	go func() { done <- s.flush(t.Context(), nil) }()
	<-entered
	clock.advance(c.config.PendingRetention)
	c.Report(t.Context(), backendReport())
	second := capture(t, c)
	if len(c.pending) != 2 || c.pending[0].id != first.id || second.id == first.id {
		t.Error("capture expired or replaced the actively flushing head")
	}
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(c.pending) != 1 || c.pending[0].id != second.id || c.dropped != 1 {
		t.Fatalf("post-lock expiry did not remove only the expired head: pending=%d dropped=%d", len(c.pending), c.dropped)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		count, err := orm.Count(t.Context(), tx, flushes.Query())
		if count != 0 {
			t.Errorf("expired absent receipt wrote %d rows", count)
		}
		return err
	})
}

func TestStorageCancellationWhileWaitingForLockDoesNotWrite(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	s.collector.Report(t.Context(), backendReport())
	b := capture(t, s.collector)
	locked, release, holder := holdTelemetryLock(t, db, schema)
	<-locked
	entered := make(chan struct{})
	transact := s.transact
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return transact(ctx, func(tx *database.Tx) error { close(entered); return fn(tx) }, options...)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.flush(ctx, nil) }()
	<-entered
	cancel()
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !storageInterrupted(ctx, err) {
		t.Fatalf("flush cancellation: %v", err)
	}
	if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id {
		t.Fatal("cancellation discarded pending identity")
	}
	if s.collector.failures != 0 {
		t.Fatalf("cancellation counted as storage failure: %d", s.collector.failures)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		count, err := orm.Count(t.Context(), tx, events.Query())
		if count != 0 {
			t.Errorf("cancelled flush wrote %d events", count)
		}
		return err
	})
}

// The channels provide a deterministic lock barrier without timing guesses.
// A separate scoped pool is unnecessary: testkit pools admit both transactions.
func holdTelemetryLock(t *testing.T, db *database.DB, schema string) (<-chan struct{}, chan struct{}, <-chan error) {
	t.Helper()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.Primary().Transaction(t.Context(), func(tx *database.Tx) error {
			if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
				return err
			}
			if _, err := orm.RequireLocked(t.Context(), tx, orm.Lock(store.Controls.Query()).Where(orm.KeyEq(store.Controls.Key(), "storage"))); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-t.Context().Done():
				return t.Context().Err()
			}
		}, database.TxOptions{Isolation: database.ReadCommitted})
	}()
	return locked, release, done
}

func TestStorageConstructionValidatesHorizonAndOwnership(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	if _, err := newStorage(s.collector, db, schema, engineConfig()); err == nil {
		t.Fatal("second storage owner accepted")
	}
	for _, test := range []struct {
		name string
		edit func(*Config)
	}{
		{"equal horizon", func(c *Config) { c.Retention.Minute = time.Hour; c.Retention.Hour = 2*time.Hour + c.FlushTimeout }},
		{"short horizon", func(c *Config) { c.Retention.Minute = time.Hour; c.Retention.Hour = time.Hour }},
		{"overflow duration", func(c *Config) { c.DrainTimeout = time.Duration(1<<63 - 1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			config := engineConfig()
			test.edit(&config)
			if _, err := newStorage(c, db, schema, config); err == nil {
				t.Fatal("unsafe retry horizon accepted")
			}
			if c.storageOwned {
				t.Fatal("failed validation claimed collector")
			}
		})
	}
}
