package telemetry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func postgresStorage(t *testing.T, collectorConfig CollectorConfig, config Config) (*storage, *database.DB, string, *testClock) {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	schema := scope.Schema()
	if _, err := telemetryMigrationRunner(t, db, schema).Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	c, clock := newCollector(t, collectorConfig)
	s, err := newStorage(c, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	return s, db, schema, clock
}

func storageTransaction(t *testing.T, db *database.DB, schema string, fn func(*database.Tx) error) {
	t.Helper()
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted}); err != nil {
		t.Fatal(err)
	}
}

type telemetryCounts struct{ runs, receipts, metrics, issues, events int64 }

func countTelemetry(t *testing.T, db *database.DB, schema string) telemetryCounts {
	t.Helper()
	var result telemetryCounts
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		if result.runs, err = orm.Count(t.Context(), tx, runs.Query()); err != nil {
			return err
		}
		if result.receipts, err = orm.Count(t.Context(), tx, flushes.Query()); err != nil {
			return err
		}
		if result.metrics, err = orm.Count(t.Context(), tx, metrics.Query()); err != nil {
			return err
		}
		if result.issues, err = orm.Count(t.Context(), tx, issues.Query()); err != nil {
			return err
		}
		result.events, err = orm.Count(t.Context(), tx, events.Query())
		return err
	})
	return result
}

func storedIssue(t *testing.T, db *database.DB, schema string, key Fingerprint) issueRow {
	t.Helper()
	var result issueRow
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		result, err = orm.RequireKey(t.Context(), tx, issues.Table, key)
		return err
	})
	return result
}

func storedReceipt(t *testing.T, db *database.DB, schema string, b batch) receiptState {
	t.Helper()
	var result receiptState
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		row, err := orm.RequireKey(t.Context(), tx, flushes.Table, receiptKey(b.id))
		if err != nil {
			return err
		}
		result, err = decodePayload(row.Result, maxReceiptPayloadBytes)
		return err
	})
	return result
}

func TestPostgresTelemetryFIFOReceiptsReplaceCumulativeBuckets(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	c := s.collector
	c.ObserveRequest(t.Context(), request())
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	first := capture(t, c)
	c.ObserveRequest(t.Context(), request())
	if err := c.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	second := capture(t, c)
	firstProgress := []ruleProgress{{Rule: RuleLatency, Bad: 1}}
	if err := s.flush(t.Context(), firstProgress); err != nil {
		t.Fatal(err)
	}
	if len(c.pending) != 1 || c.pending[0].id != second.id || s.applied != first.id {
		t.Fatal("flush did not consume only the FIFO head")
	}
	assertHistograms := func(want uint64) {
		t.Helper()
		storageTransaction(t, db, schema, func(tx *database.Tx) error {
			found, err := orm.All(t.Context(), tx, metrics.Query())
			if err != nil {
				return err
			}
			if len(found) != 2 {
				t.Fatalf("metric rows = %d", len(found))
			}
			for _, row := range found {
				histogram, err := decodePayload(row.Histogram, maxHistogramPayloadBytes)
				if err != nil {
					return err
				}
				if histogram.Count != want {
					t.Fatalf("cumulative count = %d, want %d", histogram.Count, want)
				}
			}
			return nil
		})
	}
	assertHistograms(1)
	if err := s.flush(t.Context(), []ruleProgress{{Rule: RuleLatency, Bad: 2}}); err != nil {
		t.Fatal(err)
	}
	assertHistograms(2)
	if counts := countTelemetry(t, db, schema); counts != (telemetryCounts{runs: 1, receipts: 2, metrics: 2, issues: 1, events: 2}) {
		t.Fatalf("rows = %+v", counts)
	}
	// An older delayed receipt cannot replace a newer cumulative snapshot.
	result, err := s.persist(t.Context(), first, receiptState{Rules: []ruleProgress{{Rule: RuleLatency, Bad: 9}}})
	if err != nil || !result.committed || !reflect.DeepEqual(result.state.Rules, firstProgress) {
		t.Fatal("receipt did not restore the originally committed state", err)
	}
	assertHistograms(2)
	if storedIssue(t, db, schema, fingerprint(first.issues[0])).Count != 2 {
		t.Fatal("receipt replay incremented issue count")
	}
}

func TestPostgresTelemetryCoalescesDelayedAndTiedIssues(t *testing.T) {
	s, db, schema, clock := postgresStorage(t, collectorConfig(), engineConfig())
	c := s.collector
	base := clock.Now()
	observe := func(offset time.Duration, file string) observedIssue {
		at, err := temporal.NewDateTime(base.Add(offset))
		if err != nil {
			t.Fatal(err)
		}
		return observedIssue{Source: SourceBackend, Code: "failure", Route: "example.read", Release: c.config.Release, At: at, Detail: Detail{Frames: []Frame{{File: file, Line: 1}}}}
	}
	latest := observe(30*time.Second, "new.go")
	for _, issue := range []observedIssue{latest, observe(10*time.Second, "old.go"), observe(30*time.Second, "tie.go")} {
		c.add(issue)
	}
	capture(t, c)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	assertGroup := func(count int64, first, last time.Duration, file string) {
		t.Helper()
		row := storedIssue(t, db, schema, fingerprint(latest))
		detail, err := decodePayload(row.Detail, maxDetailPayloadBytes)
		if err != nil {
			t.Fatal(err)
		}
		if row.Count != count || !row.FirstAt.UTC().Equal(base.Add(first)) || !row.LastAt.UTC().Equal(base.Add(last)) || detail.Frames[0].File != file {
			t.Fatal("coalesced timestamps, count or strict-newer detail differ")
		}
	}
	assertGroup(3, 10*time.Second, 30*time.Second, "new.go")
	for _, step := range []struct {
		offset, first, last time.Duration
		file, want          string
		count               int64
	}{
		{5 * time.Second, 5 * time.Second, 30 * time.Second, "delayed.go", "new.go", 4},
		{30 * time.Second, 5 * time.Second, 30 * time.Second, "tied.go", "new.go", 5},
		{40 * time.Second, 5 * time.Second, 40 * time.Second, "newest.go", "newest.go", 6},
	} {
		c.add(observe(step.offset, step.file))
		capture(t, c)
		if err := s.flush(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		assertGroup(step.count, step.first, step.last, step.want)
	}
	if got := countTelemetry(t, db, schema); got.issues != 1 || got.events != 6 {
		t.Fatalf("issue/sample rows = %+v", got)
	}
}

func TestPostgresTelemetryWritesAcrossInsertBatchBoundary(t *testing.T) {
	cc := collectorConfig()
	cc.MaxSeries, cc.MaxEvents = 4096, 4096
	s, db, schema, _ := postgresStorage(t, cc, engineConfig())
	count := orm.MaxInsertRows + 1
	for i := range count {
		route := http.RouteID(fmt.Sprintf("bulk.%04d", i))
		e := request()
		e.Route = route
		s.collector.ObserveRequest(t.Context(), e)
		r := backendReport()
		r.Entry.Route.Name = string(route)
		if err := s.collector.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	capture(t, s.collector)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: int64(count * 2), issues: int64(count), events: int64(count)}) {
		t.Fatalf("batched row totals = %+v", got)
	}
	if s.collector.dropped != 0 {
		t.Fatal("batched inserts lost observations")
	}
}

func capacityConfig() Config {
	config := engineConfig()
	config.Retention.MaxMetricRows, config.Retention.MaxIssueRows, config.Retention.MaxEventRows = 100, 100, 100
	config.Retention.MaxRuns, config.Retention.MaxReceiptRows = 100, 2
	config.Reads.MaxTrafficRows, config.Reads.MaxTrafficGroups = 100, 100
	return config
}

func TestPostgresTelemetryCapacityUpdatesExistingAndAppliesLossOnce(t *testing.T) {
	cc := collectorConfig()
	cc.MaxSeries, cc.MaxEvents = 128, 128
	s, db, schema, _ := postgresStorage(t, cc, capacityConfig())
	c := s.collector
	for i := range 50 {
		e := request()
		e.Route = http.RouteID(fmt.Sprintf("capacity.%03d", i))
		c.ObserveRequest(t.Context(), e)
	}
	for i := range 100 {
		r := backendReport()
		r.Entry.Route.Name = fmt.Sprintf("group.%03d", i)
		if err := c.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	capture(t, c)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	for _, route := range []http.RouteID{"capacity.000", "capacity.050"} {
		e := request()
		e.Route = route
		c.ObserveRequest(t.Context(), e)
	}
	for _, route := range []string{"group.000", "group.100"} {
		r := backendReport()
		r.Entry.Route.Name = route
		if err := c.Report(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	b := capture(t, c)
	progress := []ruleProgress{{Rule: RuleTelemetryLoss, Bad: 3}}
	if err := s.flush(t.Context(), progress); err != nil {
		t.Fatal(err)
	}
	if c.dropped != 4 {
		t.Fatalf("storage loss = %d, want two metric snapshots, one issue, one event", c.dropped)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 2, metrics: 100, issues: 100, events: 100}) {
		t.Fatalf("capped rows = %+v", got)
	}
	row := storedIssue(t, db, schema, fingerprint(b.issues[0]))
	if row.Count != 2 {
		t.Fatal("existing issue did not update at capacity")
	}
	stored := storedReceipt(t, db, schema, b)
	if stored.StorageDropped != 4 || !reflect.DeepEqual(stored.Rules, progress) {
		t.Fatal("receipt did not atomically retain storage loss/progress")
	}
	// Existing receipts reconcile even when receipt capacity is full; repeating
	// their result neither advances progress nor applies the same loss twice.
	c.mu.Lock()
	c.pending = append(c.pending, b)
	c.mu.Unlock()
	if err := s.flush(t.Context(), []ruleProgress{{Rule: RuleTelemetryLoss, Bad: 4}}); err != nil {
		t.Fatal(err)
	}
	if c.dropped != 4 || !reflect.DeepEqual(s.progress.Rules, progress) {
		t.Fatal("receipt result was applied twice")
	}
	capture(t, c)
	if err := s.flush(t.Context(), nil); !errors.Is(err, fault.Overloaded) {
		t.Fatalf("new receipt at capacity = %v", err)
	}
	if len(c.pending) != 1 || c.dropped != 4 || countTelemetry(t, db, schema).receipts != 2 {
		t.Fatal("receipt-cap failure changed admitted data")
	}
}

func TestPostgresTelemetryRunCapacityAllowsExistingRun(t *testing.T) {
	config := engineConfig()
	config.Retention.MaxRuns = 100
	s, db, schema, clock := postgresStorage(t, collectorConfig(), config)
	capture(t, s.collector)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	payload, err := encodePayload(Runtime{}, maxRuntimePayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	at, err := temporal.NewDateTime(clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	until, err := at.Add(config.Retention.Hour)
	if err != nil {
		t.Fatal(err)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		drafts := make([][]orm.Assignment[runRow], 0, 99)
		for range 99 {
			id, err := model.NewIDAt[runRow](clock.Now())
			if err != nil {
				return err
			}
			drafts = append(drafts, []orm.Assignment[runRow]{orm.KeySet(runs.Key(), id), orm.TextSet(runs.Instance, Instance("seed")), orm.TextSet(runs.Release, Release("development")), orm.TimeSet(runs.StartedAt, at), orm.TimeSet(runs.HeartbeatAt, at), orm.TimeSet(runs.RetainUntil, until), orm.JSONSet(runs.Runtime, payload)})
		}
		_, err := orm.InsertMany(t.Context(), tx, runs.Table, drafts)
		return err
	})
	capture(t, s.collector)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal("existing run rejected at capacity", err)
	}
	other, _ := newCollector(t, collectorConfig())
	writer, err := newStorage(other, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	capture(t, other)
	if err := writer.flush(t.Context(), nil); !errors.Is(err, fault.Overloaded) {
		t.Fatalf("new run at capacity = %v", err)
	}
	if got := countTelemetry(t, db, schema); got.runs != 100 || got.receipts != 2 {
		t.Fatalf("run-cap rollback = %+v", got)
	}
}

func TestPostgresTelemetryReceiptAndWritesRollbackAtomically(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	s.collector.ObserveRequest(t.Context(), request())
	if err := s.collector.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	b := capture(t, s.collector)
	original := s.transact
	abort := errors.New("injected transaction rollback")
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return original(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			return abort
		}, options...)
	}
	if err := s.flush(t.Context(), []ruleProgress{{Rule: RuleLatency, Bad: 1}}); !errors.Is(err, abort) {
		t.Fatalf("rollback = %v", err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
		t.Fatalf("partial transaction = %+v", got)
	}
	if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id || !s.applied.IsZero() || len(s.progress.Rules) != 0 {
		t.Fatal("rolled-back receipt advanced in-memory state")
	}
	s.transact = original
	if err := s.flush(t.Context(), []ruleProgress{{Rule: RuleLatency, Bad: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: 2, issues: 1, events: 1}) {
		t.Fatalf("retry after rollback = %+v", got)
	}
}

func TestPostgresCaptureClockRollbackNeverDecreasesStoredHistograms(t *testing.T) {
	for _, step := range []time.Duration{time.Minute, time.Hour} {
		t.Run(step.String(), func(t *testing.T) {
			config := engineConfig()
			config.Alerts = alertPolicy()
			config.Alerts.Rules = []Rule{RuleLatency}
			config.Alerts.Thresholds.MinimumRequests = 5
			config.Alerts.Thresholds.Latency = time.Millisecond
			s, db, schema, source := postgresStorage(t, collectorConfig(), config)
			c := s.collector
			start := source.Now()
			for range 10 {
				c.ObserveRequest(t.Context(), request())
			}
			first := capture(t, c)
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			source.advance(step)
			for range 2 {
				c.ObserveRequest(t.Context(), request())
			}
			capture(t, c)
			for _, entry := range c.buckets {
				if entry.Interval == time.Minute && entry.At.Equal(start.Truncate(time.Minute)) {
					t.Fatal("test did not evict the original minute")
				}
			}
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			source.set(start)
			c.ObserveRequest(t.Context(), request())
			rollback := capture(t, c)
			if !rollback.at.UTC().Equal(start.Add(step)) {
				t.Fatal("batch timestamp moved backwards")
			}
			minutes := 0
			for _, entry := range rollback.buckets {
				if entry.Interval == time.Minute {
					minutes++
					if !entry.At.Equal(start.Add(step).Truncate(time.Minute)) || entry.Metric.Count != 3 {
						t.Fatal("incident input included a recreated or non-current minute")
					}
				}
			}
			if minutes != 1 {
				t.Fatalf("current minute series = %d", minutes)
			}
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if len(s.progress.Rules) != 1 || s.progress.Rules[0].Bad != 0 || s.progress.Rules[0].Healthy != 0 {
				t.Fatal("old traffic bypassed the incident minimum request threshold")
			}
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				found, err := orm.All(t.Context(), tx, metrics.Query())
				if err != nil {
					return err
				}
				for _, original := range first.buckets {
					matched := false
					for _, row := range found {
						if string(row.Key) != original.Key {
							continue
						}
						matched = true
						hist, err := decodePayload(row.Histogram, maxHistogramPayloadBytes)
						if err != nil {
							return err
						}
						if hist.Count < original.Metric.Count || hist.Seconds < original.Metric.Seconds {
							return fmt.Errorf("persisted cumulative histogram decreased")
						}
						for i, count := range original.Metric.Buckets {
							if hist.Buckets[i] < count {
								return fmt.Errorf("persisted cumulative histogram bucket decreased")
							}
						}
					}
					if !matched {
						return fmt.Errorf("original persisted bucket disappeared")
					}
				}
				return nil
			})
		})
	}
}
