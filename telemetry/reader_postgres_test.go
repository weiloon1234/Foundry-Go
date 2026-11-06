package telemetry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func postgresReader(t *testing.T, config Config, routes RoutePolicy) (*Reader, *database.DB, string, temporal.DateTime) {
	t.Helper()
	_, db, schema, source := postgresStorage(t, collectorConfig(), config)
	r, err := newReader(Dependencies{DB: db, Schema: schema, Clock: source}, config, routes, &operationOwner{})
	if err != nil {
		t.Fatal(err)
	}
	return r, db, schema, retentionNow(t, db, schema)
}

func seedReaderMetric(ctx context.Context, tx *database.Tx, run model.ID[runRow], name string, at temporal.DateTime, interval int, route http.RouteID, status int, histogram store.Histogram) error {
	payload, err := encodePayload(histogram, maxHistogramPayloadBytes)
	if err != nil {
		return err
	}
	_, err = orm.Insert(ctx, tx, metrics.Table,
		orm.KeySet(metrics.Key(), store.MetricKey(digest(name))), orm.ColumnSet(metrics.RunID, run), orm.TimeSet(metrics.BucketAt, at), orm.NumberSet(metrics.IntervalSeconds, interval),
		orm.TextSet(metrics.Route, route), orm.TextSet(metrics.RoutePath, "/declared"), orm.TextSet(metrics.Method, http.GET), orm.NumberSet(metrics.Status, status), orm.TextSet(metrics.Outcome, observability.Succeeded), orm.JSONSet(metrics.Histogram, payload))
	return err
}

func seedReaderAlert(ctx context.Context, tx *database.Tx, run model.ID[runRow], name string, at temporal.DateTime, state AlertState) error {
	_, err := orm.Insert(ctx, tx, alerts.Table, orm.KeySet(alerts.Key(), AlertKey(digest(name))), orm.ColumnSet(alerts.RunID, run), orm.EnumSet(alerts.Rule, RuleLatency), orm.EnumSet(alerts.State, state), orm.NumberSet(alerts.Episode, Episode(1)), orm.TimeSet(alerts.FirstAt, at), orm.TimeSet(alerts.LastAt, at), orm.NullableTimeSetNull(alerts.RecoveredAt))
	return err
}

func readerHistogram(count uint64, seconds float64, overflow bool) store.Histogram {
	buckets := make([]uint64, len(observability.DurationBuckets()))
	if !overflow {
		for i := range buckets {
			buckets[i] = count
		}
	}
	return store.Histogram{Count: count, Seconds: seconds, Buckets: buckets}
}

func TestPostgresReaderSourceRetentionAndStablePages(t *testing.T) {
	config := retentionConfig()
	r, db, schema, now := postgresReader(t, config, RoutePolicy{})
	old := retentionAt(t, now, -2*time.Hour)
	expired := retentionAt(t, now, -4*time.Hour)
	recent := retentionAt(t, now, -time.Minute)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		for _, item := range []struct {
			name   string
			source Source
			at     temporal.DateTime
			count  int
		}{
			{"backend-old", SourceBackend, old, 1}, {"security-old", SourceSecurity, old, 1}, {"backend-expired", SourceBackend, expired, 1}, {"backend-recent", SourceBackend, recent, 3}, {"security-recent", SourceSecurity, recent, 1},
		} {
			if err := seedRetentionIssues(t.Context(), tx, item.name, item.source, item.at, item.count); err != nil {
				return err
			}
		}
		key := Fingerprint(digest("backend-recent-0"))
		detail, err := encodePayload(Detail{Frames: []Frame{{File: "handler.go", Function: "Handle", Line: 7}}}, maxDetailPayloadBytes)
		if err != nil {
			return err
		}
		_, err = orm.UpdateAll(t.Context(), tx, issues.Query().Where(orm.KeyEq(issues.Key(), key)), orm.JSONSet(issues.Detail, detail))
		return err
	})
	query := IssueQuery{Source: SourceBackend, Page: orm.Page(1, 2)}
	first, err := r.Issues(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	query.Page.Number = 2
	second, err := r.Issues(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 4 || first.Pages != 2 || second.Total != 4 || len(first.Items) != 2 || len(second.Items) != 2 {
		t.Fatalf("unexpected retained page metadata: %+v %+v", first, second)
	}
	got := append(slices.Clone(first.Items), second.Items...)
	want := []Fingerprint{Fingerprint(digest("backend-recent-0")), Fingerprint(digest("backend-recent-1")), Fingerprint(digest("backend-recent-2"))}
	slices.Sort(want)
	want = append(want, Fingerprint(digest("backend-old-0")))
	for i, item := range got {
		if item.Fingerprint != want[i] {
			t.Fatal("timestamp ties did not retain primary-key ordering")
		}
	}
	security, err := r.Issues(t.Context(), IssueQuery{Source: SourceSecurity, Page: orm.Page(1, 20)})
	if err != nil || security.Total != 1 {
		t.Fatalf("security retention: total=%d err=%v", security.Total, err)
	}
	for _, test := range []struct {
		name  string
		total int64
	}{{"backend-old-0", 1}, {"security-old-0", 0}, {"backend-expired-0", 0}, {"missing", 0}} {
		page, err := r.Events(t.Context(), EventQuery{Fingerprint: Fingerprint(digest(test.name)), Page: orm.Page(1, 20)})
		if err != nil || page.Total != test.total {
			t.Fatalf("event retention %s: total=%d err=%v", test.name, page.Total, err)
		}
	}
	for i := range got {
		if len(got[i].Detail.Frames) > 0 {
			got[i].Detail.Frames[0].File = "changed"
		}
	}
	again, err := r.Issues(t.Context(), IssueQuery{Source: SourceBackend, Page: orm.Page(1, 20)})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range again.Items {
		if len(item.Detail.Frames) > 0 && item.Detail.Frames[0].File != "handler.go" {
			t.Fatal("read result retained caller-owned detail mutation")
		}
	}
	if counts := countTelemetry(t, db, schema); counts.issues != 7 || counts.events != 7 {
		t.Fatal("reader pruned or rewrote retained storage")
	}
}

func TestPostgresReaderOverviewUsesServerRetentionAndSignalsCaps(t *testing.T) {
	config := retentionConfig()
	config.Reads.MaxOverviewRuns = 1
	config.Reads.MaxOverviewAlerts = 1
	r, db, schema, now := postgresReader(t, config, RoutePolicy{})
	until := retentionAt(t, now, 24*time.Hour)
	stale := retentionAt(t, now, -5*time.Minute)
	expired := retentionAt(t, now, -5*time.Hour)
	oldHour := retentionAt(t, now, -3*time.Hour)
	var newest model.ID[runRow]
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		newest, err = seedRetentionRun(t.Context(), tx, now, until)
		if err != nil {
			return err
		}
		old, err := seedRetentionRun(t.Context(), tx, stale, until)
		if err != nil {
			return err
		}
		dead, err := seedRetentionRun(t.Context(), tx, expired, until)
		if err != nil {
			return err
		}
		for _, item := range []struct {
			run   model.ID[runRow]
			name  string
			state AlertState
		}{{newest, "live-open", AlertStateOpen}, {old, "live-ack", AlertStateAcknowledged}, {dead, "expired", AlertStateOpen}} {
			if err := seedReaderAlert(t.Context(), tx, item.run, item.name, now, item.state); err != nil {
				return err
			}
		}
		if err := seedRetentionMetrics(t.Context(), tx, newest, "expired-minute", retentionAt(t, now, -2*time.Hour), 60, 1); err != nil {
			return err
		}
		return seedRetentionMetrics(t.Context(), tx, newest, "retained-hour", oldHour, 3600, 1)
	})
	// The collector's framework clock is deliberately years ahead. Reader cutoffs
	// and stale heartbeats still use the primary database clock.
	config2 := config
	config2.Reads.MaxOverviewRuns = 10
	config2.Reads.MaxOverviewAlerts = 10
	clock := &testClock{}
	clock.set(now.UTC().Add(10 * 365 * 24 * time.Hour))
	full, err := newReader(Dependencies{DB: db, Schema: schema, Clock: clock}, config2, RoutePolicy{}, &operationOwner{})
	if err != nil {
		t.Fatal(err)
	}
	limited, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !limited.Truncated || len(limited.Runs) != 1 || len(limited.Alerts) != 1 || limited.Runs[0].ID != model.IDFromBytes[Run](newest.Bytes()) {
		t.Fatal("overview omitted caps or newest-run ordering")
	}
	all, err := full.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if all.Truncated || len(all.Runs) != 2 || len(all.Alerts) != 2 || all.Runs[0].Stale || !all.Runs[1].Stale {
		t.Fatal("overview retention, window independence or stale cutoff changed")
	}
	if oldest, ok := all.OldestBucket.Get(); !ok || !oldest.UTC().Equal(oldHour.UTC()) {
		t.Fatal("oldest bucket included expired minute data")
	}
	ack, err := full.Alerts(t.Context(), AlertQuery{State: value.Set(AlertStateAcknowledged), Page: orm.Page(1, 20)})
	if err != nil || ack.Total != 1 || ack.Items[0].Key != AlertKey(digest("live-ack")) {
		t.Fatalf("incident filter or parent-run retention: %+v %v", ack, err)
	}
	page1, err := full.Alerts(t.Context(), AlertQuery{Page: orm.Page(1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := full.Alerts(t.Context(), AlertQuery{Page: orm.Page(2, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 2 || page2.Total != 2 || page1.Items[0].Key >= page2.Items[0].Key {
		t.Fatal("incident timestamp ties lack stable key ordering")
	}
}

func TestPostgresReaderTrafficAggregatesBeforeQuantilesAndHonorsWindows(t *testing.T) {
	config := retentionConfig()
	routes := RoutePolicy{ExcludedRoutes: []http.RouteID{"excluded"}}
	r, db, schema, now := postgresReader(t, config, routes)
	routes.ExcludedRoutes[0] = "included"
	at := retentionAt(t, now, -time.Minute)
	partialTime := now.UTC().Add(-14 * time.Minute).Truncate(time.Minute)
	partial, err := temporal.NewDateTime(partialTime)
	if err != nil {
		t.Fatal(err)
	}
	short := readerHistogram(2, .002, false)
	slow := readerHistogram(1, 100, true)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		one, err := seedRetentionRun(t.Context(), tx, now, retentionAt(t, now, time.Hour))
		if err != nil {
			return err
		}
		two, err := seedRetentionRun(t.Context(), tx, now, retentionAt(t, now, time.Hour))
		if err != nil {
			return err
		}
		for _, item := range []struct {
			name     string
			run      model.ID[runRow]
			at       temporal.DateTime
			interval int
			route    http.RouteID
			status   int
			hist     store.Histogram
		}{
			{"one", one, partial, 60, "included", 200, short}, {"two", two, at, 60, "included", 200, slow}, {"failed", one, at, 60, "failed", 503, short}, {"excluded", one, at, 60, "excluded", 200, short},
			{"hour", one, retentionAt(t, now, -time.Hour), 3600, "hour", 200, slow}, {"expired-minute", one, retentionAt(t, now, -2*time.Hour), 60, "expired", 200, short},
		} {
			if err := seedReaderMetric(t.Context(), tx, item.run, item.name, item.at, item.interval, item.route, item.status, item.hist); err != nil {
				return err
			}
		}
		return nil
	})
	traffic, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 15}})
	if err != nil {
		t.Fatal(err)
	}
	if traffic.Truncated || len(traffic.Items) != 2 || traffic.Summary.Count != 5 || traffic.Summary.Failed != 2 || !traffic.Summary.P95.Overflow {
		t.Fatalf("aggregate summary = %+v", traffic)
	}
	if traffic.Items[0].Route != "included" || traffic.Items[0].Count != 3 || !traffic.Items[0].P95.Overflow || traffic.Items[0].P50.Overflow {
		t.Fatal("histograms were not summed before estimation")
	}
	combined := readerHistogram(5, .004, false)
	for i := range combined.Buckets {
		combined.Buckets[i] = 4
	}
	want, err := observability.EstimateQuantile(combined.Buckets, combined.Count, .95)
	if err != nil || traffic.Summary.P95 != want {
		t.Fatal("summary did not estimate the combined histogram")
	}
	all, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 15}, IncludeExcluded: true})
	if err != nil || all.Summary.Count != 7 {
		t.Fatalf("include excluded: %+v %v", all, err)
	}
	hour, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 61}})
	if err != nil || len(hour.Items) != 1 || hour.Items[0].Route != "hour" {
		t.Fatalf("hour selection: %+v %v", hour, err)
	}
	empty, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Summary.Count == 0 && empty.Summary.P95.Available {
		t.Fatal("empty traffic fabricated an available percentile")
	}
}

func TestPostgresReaderTrafficRowAndGroupTruncation(t *testing.T) {
	config := retentionConfig()
	config.Reads.MaxTrafficRows = 2
	config.Reads.MaxTrafficGroups = 1
	r, db, schema, now := postgresReader(t, config, RoutePolicy{})
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		run, err := seedRetentionRun(t.Context(), tx, now, retentionAt(t, now, time.Hour))
		if err != nil {
			return err
		}
		for i := range 3 {
			if err := seedReaderMetric(t.Context(), tx, run, fmt.Sprint("limited", i), retentionAt(t, now, time.Duration(i-3)*time.Minute), 60, http.RouteID(fmt.Sprint("route", i)), 200, readerHistogram(1, .001, false)); err != nil {
				return err
			}
		}
		return nil
	})
	out, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 15}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Items) != 1 || out.Summary.Count != 1 || out.Items[0].Route != "route0" {
		t.Fatal("read/group caps failed to disclose omitted traffic")
	}
	config.Reads.MaxTrafficGroups = 2
	r2, err := newReader(Dependencies{DB: db, Schema: schema}, config, RoutePolicy{}, &operationOwner{})
	if err != nil {
		t.Fatal(err)
	}
	out, err = r2.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 15}})
	if err != nil || !out.Truncated || len(out.Items) != 2 || out.Summary.Count != 2 {
		t.Fatalf("row lookahead bound: %+v %v", out, err)
	}
}

func TestPostgresReaderRejectsCorruptHistogramStorage(t *testing.T) {
	for _, test := range []struct {
		name    string
		version int
		hist    store.Histogram
	}{
		// An unsupported payload version can't be stored: the schema's CHECK rejects it
		// (TestPostgresTelemetryMigrationRejectsInvalidPayloadVersions).
		{"wrong shape", 1, store.Histogram{Count: 1, Seconds: .001, Buckets: []uint64{1}}},
		{"negative seconds", 1, readerHistogram(1, -1, false)},
		{"count mismatch", 1, readerHistogram(0, 0, false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, db, schema, now := postgresReader(t, retentionConfig(), RoutePolicy{})
			if test.name == "count mismatch" {
				test.hist.Buckets[0] = 1
			}
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				run, err := seedRetentionRun(t.Context(), tx, now, retentionAt(t, now, time.Hour))
				if err != nil {
					return err
				}
				if err := seedReaderMetric(t.Context(), tx, run, "corrupt", now, 60, "corrupt", 200, readerHistogram(1, .001, false)); err != nil {
					return err
				}
				payload, err := value.NewJSON(store.Envelope[store.Histogram]{Version: test.version, Data: test.hist})
				if err != nil {
					return err
				}
				_, err = orm.UpdateAll(t.Context(), tx, metrics.Query().Where(orm.KeyEq(metrics.Key(), store.MetricKey(digest("corrupt")))), orm.JSONSet(metrics.Histogram, payload))
				return err
			})
			out, err := r.Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 15}})
			if !errors.Is(err, fault.Internal) || len(out.Items) != 0 || out.Summary.P95.Available {
				t.Fatalf("corrupt storage fabricated a successful result: %+v %v", out, err)
			}
		})
	}
}

func TestPostgresReaderOwnsAdmittedWorkUntilDatabaseExit(t *testing.T) {
	config := retentionConfig()
	config.Reads.MaxConcurrent = 1
	config.Reads.Timeout = time.Minute
	r, _, _, _ := postgresReader(t, config, RoutePolicy{})
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	result := make(chan error, 1)
	go func() {
		result <- r.read(t.Context(), func(context.Context, *database.Tx, temporal.DateTime) error { close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not enter database callback")
	}
	if _, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}}); !errors.Is(err, fault.Overloaded) {
		unblock()
		t.Fatalf("full read capacity: %v", err)
	}
	done := r.owner.seal()
	select {
	case <-done:
		unblock()
		t.Fatal("owner completed under admitted database work")
	default:
	}
	if _, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}}); !errors.Is(err, fault.Closed) {
		unblock()
		t.Fatalf("sealed read admission: %v", err)
	}
	unblock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owner retained completed reader")
	}
}

func TestPostgresReaderTimeoutCancelsBlockedReadAndReleasesCapacity(t *testing.T) {
	config := retentionConfig()
	config.Reads.MaxConcurrent = 1
	config.Reads.Timeout = 100 * time.Millisecond
	r, db, schema, _ := postgresReader(t, config, RoutePolicy{})
	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	exited := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	go func() {
		defer close(exited)
		holder <- db.Primary().Transaction(t.Context(), func(tx *database.Tx) error {
			if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
				return err
			}
			// ORM row locks do not block MVCC SELECT. This test-only table
			// lock stays held until the read has returned its deadline fault.
			if _, err := tx.Exec(t.Context(), `LOCK TABLE foundry_telemetry_runs IN ACCESS EXCLUSIVE MODE`); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	// Every assertion belongs to the test goroutine. Fatal in a transaction
	// callback invokes Goexit and is correctly contained as fault.Panicked.
	t.Cleanup(func() { unlock(); <-exited })
	select {
	case <-locked:
	case err := <-holder:
		t.Fatalf("table lock failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("table lock was not acquired")
	}
	out, readErr := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}})
	unlock()
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(readErr, fault.Timeout) || !errors.Is(readErr, context.DeadlineExceeded) || errors.Is(readErr, fault.Panicked) {
		t.Fatalf("blocked read did not return its timeout: %v", readErr)
	}
	if len(out.Runs) != 0 || len(out.Alerts) != 0 {
		t.Fatal("timed-out read returned partial results")
	}
	// Recovery must not race the deliberately tiny timeout used above.
	r.config.Reads.Timeout = 10 * time.Second
	if _, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}}); err != nil {
		t.Fatalf("timed-out read retained capacity: %v", err)
	}
}

func TestPostgresReaderCanceledContextOwnsDriverClassification(t *testing.T) {
	for _, code := range []database.Code{database.Canceled, database.Unavailable} {
		t.Run(string(code), func(t *testing.T) {
			config := retentionConfig()
			config.Reads.Timeout = time.Minute
			config.Reads.MaxConcurrent = 1
			r, _, _, _ := postgresReader(t, config, RoutePolicy{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- r.read(ctx, func(attempt context.Context, _ *database.Tx, _ temporal.DateTime) error {
					close(entered)
					<-attempt.Done()
					return database.NewError("query", code)
				})
			}()
			waitModuleSignal(t, entered, "reader did not enter its callback")
			cancel()
			err := <-done
			if !errors.Is(err, fault.Timeout) || !errors.Is(err, context.Canceled) || errors.Is(err, fault.Panicked) {
				t.Fatalf("reader context classification: %v", err)
			}
			if _, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}}); err != nil {
				t.Fatalf("canceled read retained capacity: %v", err)
			}
		})
	}
}

func TestPostgresReaderEventsUseStableKeysWithoutAnIssueGroup(t *testing.T) {
	r, db, schema, now := postgresReader(t, retentionConfig(), RoutePolicy{})
	key := Fingerprint(digest("orphan-samples"))
	var want []string
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		detail, err := encodePayload(Detail{Frames: []Frame{{File: "sample.go", Line: 4}}}, maxDetailPayloadBytes)
		if err != nil {
			return err
		}
		for range 3 {
			id, err := model.NewIDAt[eventRow](now.UTC())
			if err != nil {
				return err
			}
			want = append(want, id.String())
			if _, err := orm.Insert(t.Context(), tx, events.Table, orm.KeySet(events.Key(), id), orm.TextSet(events.Fingerprint, key), orm.EnumSet(events.Source, SourceBackend), orm.TimeSet(events.CreatedAt, now), orm.JSONSet(events.Detail, detail)); err != nil {
				return err
			}
		}
		return nil
	})
	slices.Sort(want)
	for i := range 3 {
		page, err := r.Events(t.Context(), EventQuery{Fingerprint: key, Page: orm.Page(i+1, 1)})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 3 || page.Pages != 3 || len(page.Items) != 1 || page.Items[0].ID.String() != want[i] || !page.Items[0].CreatedAt.UTC().Equal(now.UTC()) {
			t.Fatal("event pagination lost stable key order or timestamp precision")
		}
		page.Items[0].Detail.Frames[0].File = "mutated"
	}
	again, err := r.Events(t.Context(), EventQuery{Fingerprint: key, Page: orm.Page(1, 3)})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range again.Items {
		if event.Detail.Frames[0].File != "sample.go" {
			t.Fatal("event detail shares mutable snapshot storage")
		}
	}
}

func TestPostgresReaderRuntimeAndEventSnapshotsAreOwned(t *testing.T) {
	r, db, schema, now := postgresReader(t, retentionConfig(), RoutePolicy{})
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		run, err := seedRetentionRun(t.Context(), tx, now, retentionAt(t, now, time.Hour))
		if err != nil {
			return err
		}
		payload, err := encodePayload(Runtime{Samples: []Sample{{Name: "test_total", Value: 1}}}, maxRuntimePayloadBytes)
		if err != nil {
			return err
		}
		if _, err := orm.UpdateAll(t.Context(), tx, runs.Query().Where(orm.KeyEq(runs.Key(), run)), orm.JSONSet(runs.Runtime, payload)); err != nil {
			return err
		}
		return seedRetentionIssues(t.Context(), tx, "owned", SourceBackend, now, 1)
	})
	first, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}})
	if err != nil {
		t.Fatal(err)
	}
	first.Runs[0].Runtime.Samples[0].Value = 99
	again, err := r.Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 15}})
	if err != nil || again.Runs[0].Runtime.Samples[0].Value != 1 {
		t.Fatal("runtime read reused mutable sample storage")
	}
	query := EventQuery{Fingerprint: Fingerprint(digest("owned-0")), Page: orm.Page(1, 20)}
	page, err := r.Events(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := page.Items[0].ID
	page.Items[0].ID = model.ID[Event]{}
	repeat, err := r.Events(t.Context(), query)
	if err != nil || repeat.Items[0].ID != want || reflect.DeepEqual(page.Items, repeat.Items) {
		t.Fatal("event result reused mutable row storage")
	}
}
