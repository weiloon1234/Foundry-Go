package telemetry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func retentionNow(t *testing.T, db *database.DB, schema string) temporal.DateTime {
	t.Helper()
	var now temporal.DateTime
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		now, err = serverTime(t.Context(), tx)
		return err
	})
	return now
}

func retentionAt(t *testing.T, now temporal.DateTime, delta time.Duration) temporal.DateTime {
	t.Helper()
	at, err := now.Add(delta)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func retentionConfig() Config {
	config := engineConfig()
	config.Retention.Minute = time.Hour
	config.Retention.Hour = 4 * time.Hour
	config.Retention.Issue = 3 * time.Hour
	config.Retention.Security = time.Hour
	return config
}

func insertRetentionRows[M any, K comparable](ctx context.Context, tx *database.Tx, table orm.Table[M, K], drafts [][]orm.Assignment[M]) error {
	for len(drafts) > 0 {
		n := min(len(drafts), orm.MaxInsertRows)
		if _, err := orm.InsertMany(ctx, tx, table, drafts[:n]); err != nil {
			return err
		}
		drafts = drafts[n:]
	}
	return nil
}

func seedRetentionRun(ctx context.Context, tx *database.Tx, now, until temporal.DateTime) (model.ID[runRow], error) {
	id, err := model.NewIDAt[runRow](now.UTC())
	if err != nil {
		return id, err
	}
	payload, err := encodePayload(Runtime{}, maxRuntimePayloadBytes)
	if err != nil {
		return id, err
	}
	_, err = orm.Insert(ctx, tx, runs.Table, orm.KeySet(runs.Key(), id), orm.TextSet(runs.Instance, Instance("retention-test")), orm.TextSet(runs.Release, Release("test")), orm.TimeSet(runs.StartedAt, now), orm.TimeSet(runs.HeartbeatAt, now), orm.TimeSet(runs.RetainUntil, until), orm.JSONSet(runs.Runtime, payload))
	return id, err
}

func seedRetentionMetrics(ctx context.Context, tx *database.Tx, run model.ID[runRow], prefix string, at temporal.DateTime, interval, count int) error {
	histogram, err := encodePayload(store.Histogram{Count: 1, Seconds: time.Hour.Seconds(), Buckets: make([]uint64, len(observability.DurationBuckets()))}, maxHistogramPayloadBytes)
	if err != nil {
		return err
	}
	drafts := make([][]orm.Assignment[metricRow], 0, count)
	for i := range count {
		key := store.MetricKey(digest(fmt.Sprintf("%s-%d", prefix, i)))
		drafts = append(drafts, []orm.Assignment[metricRow]{orm.KeySet(metrics.Key(), key), orm.ColumnSet(metrics.RunID, run), orm.TimeSet(metrics.BucketAt, at), orm.NumberSet(metrics.IntervalSeconds, interval), orm.TextSet(metrics.Route, http.RouteID("retention.test")), orm.TextSet(metrics.RoutePath, "/retention"), orm.TextSet(metrics.Method, http.GET), orm.NumberSet(metrics.Status, 200), orm.TextSet(metrics.Outcome, observability.Succeeded), orm.JSONSet(metrics.Histogram, histogram)})
	}
	return insertRetentionRows(ctx, tx, metrics.Table, drafts)
}

func seedRetentionIssues(ctx context.Context, tx *database.Tx, prefix string, source Source, at temporal.DateTime, count int) error {
	detail, err := encodePayload(Detail{}, maxDetailPayloadBytes)
	if err != nil {
		return err
	}
	issueDrafts := make([][]orm.Assignment[issueRow], 0, count)
	eventDrafts := make([][]orm.Assignment[eventRow], 0, count)
	for i := range count {
		key := Fingerprint(digest(fmt.Sprintf("%s-%d", prefix, i)))
		id, err := model.NewIDAt[eventRow](at.UTC())
		if err != nil {
			return err
		}
		issueDrafts = append(issueDrafts, []orm.Assignment[issueRow]{orm.KeySet(issues.Key(), key), orm.EnumSet(issues.Source, source), orm.TextSet(issues.Code, "retention.test"), orm.TextSet(issues.Route, http.RouteID("retention.test")), orm.TextSet(issues.Release, Release("test")), orm.NumberSet(issues.Count, int64(1)), orm.TimeSet(issues.FirstAt, at), orm.TimeSet(issues.LastAt, at), orm.JSONSet(issues.Detail, detail)})
		eventDrafts = append(eventDrafts, []orm.Assignment[eventRow]{orm.KeySet(events.Key(), id), orm.TextSet(events.Fingerprint, key), orm.EnumSet(events.Source, source), orm.TimeSet(events.CreatedAt, at), orm.JSONSet(events.Detail, detail)})
	}
	if err := insertRetentionRows(ctx, tx, issues.Table, issueDrafts); err != nil {
		return err
	}
	return insertRetentionRows(ctx, tx, events.Table, eventDrafts)
}

func seedRetentionReceipts(ctx context.Context, tx *database.Tx, run model.ID[runRow], stored, until temporal.DateTime, count int) error {
	payload, err := encodePayload(receiptState{}, maxReceiptPayloadBytes)
	if err != nil {
		return err
	}
	drafts := make([][]orm.Assignment[flushRow], 0, count)
	for range count {
		id, err := model.NewIDAt[flushRow](stored.UTC())
		if err != nil {
			return err
		}
		drafts = append(drafts, []orm.Assignment[flushRow]{orm.KeySet(flushes.Key(), id), orm.ColumnSet(flushes.RunID, run), orm.TimeSet(flushes.CreatedAt, stored), orm.TimeSet(flushes.StoredAt, stored), orm.TimeSet(flushes.RetainUntil, until), orm.JSONSet(flushes.Result, payload)})
	}
	return insertRetentionRows(ctx, tx, flushes.Table, drafts)
}

func TestPostgresTelemetryPruneUsesSourceAndBucketCutoffs(t *testing.T) {
	s, db, schema, wall := postgresStorage(t, collectorConfig(), retentionConfig())
	now := retentionNow(t, db, schema)
	old := retentionAt(t, now, -2*time.Hour)
	ancient := retentionAt(t, now, -5*time.Hour)
	recent := retentionAt(t, now, -30*time.Minute)
	until := retentionAt(t, now, 24*time.Hour)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		run, err := seedRetentionRun(t.Context(), tx, now, until)
		if err != nil {
			return err
		}
		for _, sample := range []struct {
			name     string
			interval int
			at       temporal.DateTime
		}{{"old-minute", 60, old}, {"kept-hour", 3600, old}, {"old-hour", 3600, ancient}, {"recent-minute", 60, recent}} {
			if err := seedRetentionMetrics(t.Context(), tx, run, sample.name, sample.at, sample.interval, 1); err != nil {
				return err
			}
		}
		for _, sample := range []struct {
			name   string
			source Source
			at     temporal.DateTime
		}{{"old-security", SourceSecurity, old}, {"kept-backend", SourceBackend, old}, {"kept-browser", SourceBrowser, old}, {"old-backend", SourceBackend, ancient}, {"recent-security", SourceSecurity, recent}} {
			if err := seedRetentionIssues(t.Context(), tx, sample.name, sample.source, sample.at, 1); err != nil {
				return err
			}
		}
		return nil
	})
	wall.set(now.UTC().Add(365 * 24 * time.Hour))
	got, err := s.prune(t.Context())
	if err != nil || got != (PruneResult{Metrics: 2, Issues: 2, Events: 2}) {
		t.Fatalf("prune = %+v, %v", got, err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, metrics: 2, issues: 3, events: 3}) {
		t.Fatalf("remaining rows = %+v", got)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		controlCount, err := orm.Count(t.Context(), tx, store.Controls.Query())
		if err != nil {
			return err
		}
		if controlCount != 1 {
			return fmt.Errorf("pruning removed the storage control row")
		}
		for _, name := range []string{"kept-backend", "kept-browser", "recent-security"} {
			exists, err := orm.Exists(t.Context(), tx, issues.Query().Where(orm.KeyEq(issues.Key(), Fingerprint(digest(name+"-0")))))
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("retained source group was removed")
			}
		}
		return nil
	})
}

func TestPostgresTelemetryPruneBoundsDelayedWorkAndRuns(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), retentionConfig())
	now := retentionNow(t, db, schema)
	old := retentionAt(t, now, -5*time.Hour)
	expired := retentionAt(t, now, -time.Hour)
	future := retentionAt(t, now, 24*time.Hour)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		run, err := seedRetentionRun(t.Context(), tx, now, future)
		if err != nil {
			return err
		}
		if err := seedRetentionMetrics(t.Context(), tx, run, "bounded", old, 60, 501); err != nil {
			return err
		}
		if err := seedRetentionIssues(t.Context(), tx, "bounded", SourceBackend, old, 501); err != nil {
			return err
		}
		return seedRetentionReceipts(t.Context(), tx, run, old, expired, 501)
	})
	first, err := s.prune(t.Context())
	if err != nil || first != (PruneResult{Metrics: 500, Issues: 500, Events: 500, Receipts: 500}) {
		t.Fatalf("bounded prune = %+v, %v", first, err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: 1, issues: 1, events: 1}) {
		t.Fatalf("bounded remainder = %+v", got)
	}
	second, err := s.prune(t.Context())
	if err != nil || second != (PruneResult{Metrics: 1, Issues: 1, Events: 1, Receipts: 1}) {
		t.Fatalf("remaining prune = %+v, %v", second, err)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		for range 51 {
			if _, err := seedRetentionRun(t.Context(), tx, old, expired); err != nil {
				return err
			}
		}
		return nil
	})
	third, err := s.prune(t.Context())
	if err != nil || third != (PruneResult{Runs: 50}) {
		t.Fatalf("run prune = %+v, %v", third, err)
	}
	fourth, err := s.prune(t.Context())
	if err != nil || fourth != (PruneResult{Runs: 1}) {
		t.Fatalf("remaining run prune = %+v, %v", fourth, err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1}) {
		t.Fatalf("unexpired run changed = %+v", got)
	}
}

func TestPostgresTelemetryReceiptHorizonSurvivesClockSkewAndShorterPruner(t *testing.T) {
	s, db, schema, wall := postgresStorage(t, collectorConfig(), engineConfig())
	now := retentionNow(t, db, schema)
	wall.set(now.UTC().Add(-365 * 24 * time.Hour))
	b := capture(t, s.collector)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	var receipt flushRow
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		receipt, err = orm.RequireKey(t.Context(), tx, flushes.Table, receiptKey(b.id))
		return err
	})
	if receipt.CreatedAt.UTC().Equal(receipt.StoredAt.UTC()) || receipt.StoredAt.UTC().Before(now.UTC()) || receipt.RetainUntil.UTC().Sub(receipt.StoredAt.UTC()) != s.horizon {
		t.Fatal("receipt retention followed captured time instead of PostgreSQL time")
	}
	shortCollector := collectorConfig()
	shortCollector.PendingRetention = time.Minute
	shortConfig := engineConfig()
	shortConfig.Retention.Minute = time.Hour
	shortConfig.Retention.Hour = time.Hour
	c, shortWall := newCollector(t, shortCollector)
	short, err := newStorage(c, db, schema, shortConfig)
	if err != nil {
		t.Fatal(err)
	}
	shortWall.set(now.UTC().Add(365 * 24 * time.Hour))
	expired := retentionAt(t, now, -time.Hour)
	olderThanShortHorizon := retentionAt(t, now, -10*time.Minute)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		if _, err := orm.UpdateAll(t.Context(), tx, runs.Query().Where(orm.KeyEq(runs.Key(), s.runKey())), orm.TimeSet(runs.RetainUntil, expired)); err != nil {
			return err
		}
		_, err := orm.UpdateAll(t.Context(), tx, flushes.Query().Where(orm.KeyEq(flushes.Key(), receipt.ID)), orm.TimeSet(flushes.StoredAt, olderThanShortHorizon))
		return err
	})
	got, err := short.prune(t.Context())
	if err != nil || got != (PruneResult{}) {
		t.Fatalf("shorter pruner removed a protected receipt/run: %+v, %v", got, err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1}) {
		t.Fatalf("protected rows = %+v", got)
	}
	stored := retentionAt(t, now, -3*time.Hour)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.UpdateAll(t.Context(), tx, flushes.Query().Where(orm.KeyEq(flushes.Key(), receipt.ID)), orm.TimeSet(flushes.StoredAt, stored), orm.TimeSet(flushes.RetainUntil, expired))
		return err
	})
	got, err = short.prune(t.Context())
	if err != nil || got != (PruneResult{Receipts: 1, Runs: 1}) {
		t.Fatalf("expired horizon prune = %+v, %v", got, err)
	}
}

func TestPostgresTelemetryRunCascadeHonorsLoweredChildCaps(t *testing.T) {
	for _, kind := range []string{"receipts", "metrics"} {
		t.Run(kind, func(t *testing.T) {
			_, db, schema, _ := postgresStorage(t, collectorConfig(), retentionConfig())
			now := retentionNow(t, db, schema)
			old := retentionAt(t, now, -5*time.Hour)
			expired := retentionAt(t, now, -time.Hour)
			var run model.ID[runRow]
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				var err error
				run, err = seedRetentionRun(t.Context(), tx, old, expired)
				if err != nil {
					return err
				}
				metricCount := 100
				if kind == "metrics" {
					metricCount = 101
				}
				if err := seedRetentionMetrics(t.Context(), tx, run, kind, now, 60, metricCount); err != nil {
					return err
				}
				if kind == "receipts" {
					return seedRetentionReceipts(t.Context(), tx, run, old, expired, 601)
				}
				return nil
			})
			lowered := retentionConfig()
			lowered.Retention.MaxMetricRows = 100
			lowered.Retention.MaxReceiptRows = 100
			lowered.Reads.MaxTrafficRows = 100
			lowered.Reads.MaxTrafficGroups = 100
			c, _ := newCollector(t, collectorConfig())
			pruner, err := newStorage(c, db, schema, lowered)
			if err != nil {
				t.Fatal(err)
			}
			got, err := pruner.prune(t.Context())
			want := PruneResult{}
			if kind == "receipts" {
				want.Receipts = 500
			}
			if err != nil || got != want {
				t.Fatalf("over-cap cascade = %+v, %v", got, err)
			}
			if countTelemetry(t, db, schema).runs != 1 {
				t.Fatal("lowered child cap allowed an oversized cascade")
			}
			if kind == "metrics" {
				storageTransaction(t, db, schema, func(tx *database.Tx) error {
					_, err := orm.UpdateAll(t.Context(), tx, metrics.Query().Where(orm.KeyEq(metrics.Key(), store.MetricKey(digest(kind+"-0")))), orm.TimeSet(metrics.BucketAt, old))
					return err
				})
				want = PruneResult{Metrics: 101, Runs: 1}
			} else {
				want = PruneResult{Metrics: 100, Receipts: 101, Runs: 1}
			}
			got, err = pruner.prune(t.Context())
			if err != nil || got != want {
				t.Fatalf("bounded cascade counts = %+v, want %+v, %v", got, want, err)
			}
			if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
				t.Fatalf("cascade left rows = %+v", got)
			}
		})
	}
}

func TestPostgresTelemetryConcurrentPruningAndAdmission(t *testing.T) {
	config := retentionConfig()
	config.Retention.Issue = time.Hour
	s, db, schema, wall := postgresStorage(t, collectorConfig(), config)
	now := retentionNow(t, db, schema)
	wall.set(now.UTC())
	old := retentionAt(t, now, -2*time.Hour)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		return seedRetentionIssues(t.Context(), tx, "concurrent-expired", SourceBackend, old, 50)
	})
	c, _ := newCollector(t, collectorConfig())
	pruner, err := newStorage(c, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	const iterations = 16
	errors := make(chan error, 2)
	go func() {
		for range iterations {
			s.collector.ObserveRequest(t.Context(), request())
			if err := s.collector.Report(t.Context(), backendReport()); err != nil {
				errors <- err
				return
			}
			if err := s.collector.capture(nil); err != nil {
				errors <- err
				return
			}
			if err := s.flush(t.Context(), nil); err != nil {
				errors <- err
				return
			}
		}
		errors <- nil
	}()
	go func() {
		for range iterations {
			if _, err := pruner.prune(t.Context()); err != nil {
				errors <- err
				return
			}
		}
		errors <- nil
	}()
	for range 2 {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		return
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: iterations, metrics: 2, issues: 1, events: iterations}) {
		t.Fatalf("concurrent rows = %+v", got)
	}
	key := fingerprint(observedIssue{Source: SourceBackend, Code: "*example.Failure", Route: "example.read", Release: s.collector.config.Release})
	if got := storedIssue(t, db, schema, key).Count; got != iterations {
		t.Fatalf("concurrent issue count = %d", got)
	}
}
