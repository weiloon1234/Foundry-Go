package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
)

func seedAdmissionRuns(t *testing.T, db *database.DB, schema string, count int, state foundation.State) []model.ID[runRow] {
	t.Helper()
	now := retentionNow(t, db, schema)
	until := retentionAt(t, now, 30*24*time.Hour)
	payload, err := encodePayload(Runtime{Health: Health{State: state}}, maxRuntimePayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]model.ID[runRow], count)
	drafts := make([][]orm.Assignment[runRow], count)
	for i := range count {
		at := retentionAt(t, now, -4*time.Hour+time.Duration(i)*time.Second)
		keys[i], err = model.NewIDAt[runRow](at.UTC())
		if err != nil {
			t.Fatal(err)
		}
		drafts[i] = []orm.Assignment[runRow]{orm.Set(runs.Key(), keys[i]), orm.Set(runs.Instance, Instance("default")), orm.Set(runs.Release, Release("development")), orm.Set(runs.StartedAt, at), orm.Set(runs.HeartbeatAt, at), orm.Set(runs.RetainUntil, until), orm.Set(runs.Runtime, payload)}
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error { return insertRetentionRows(t.Context(), tx, runs.Table, drafts) })
	return keys
}

func assertAdmissionRun(t *testing.T, db *database.DB, schema string, key model.ID[runRow], want bool) {
	t.Helper()
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		got, err := orm.Exists(t.Context(), tx, runs.Query().Where(orm.Eq(runs.Key(), key)))
		if err == nil && got != want {
			t.Errorf("run existence = %v, want %v", got, want)
		}
		return err
	})
}

func TestPostgresTelemetryAdmissionReclaimsOnlyNeededOldestEmptyRuns(t *testing.T) {
	config := engineConfig()
	config.Retention.MaxRuns = 100
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	keys := seedAdmissionRuns(t, db, schema, 102, foundation.Stopping)
	capture(t, s.collector)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema).runs; got != 100 {
		t.Fatalf("runs = %d", got)
	}
	for i, key := range keys {
		assertAdmissionRun(t, db, schema, key, i >= 3)
	}
}

func TestPostgresTelemetryAdmissionKeepsProtectedRuns(t *testing.T) {
	for _, reason := range []string{"young", "metric", "alert", "receipt", "running", "missing-state", "malformed-state", "quiet-policy"} {
		t.Run(reason, func(t *testing.T) {
			config := engineConfig()
			config.Retention.MaxRuns = 100
			if reason == "quiet-policy" {
				config.Retention.EmptyRunQuietPeriod = 5 * time.Hour
			}
			s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
			keys := seedAdmissionRuns(t, db, schema, 100, foundation.Stopping)
			now := retentionNow(t, db, schema)
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				query := runs.Query().Where(orm.Eq(runs.Key(), keys[0]))
				switch reason {
				case "young":
					_, err := orm.UpdateAll(t.Context(), tx, query, orm.Set(runs.HeartbeatAt, retentionAt(t, now, -30*time.Minute)))
					return err
				case "metric":
					return seedRetentionMetrics(t.Context(), tx, keys[0], "protected", now, 60, 1)
				case "alert":
					_, err := orm.Insert(t.Context(), tx, alerts.Table, orm.Set(alerts.Key(), AlertKey(digest("protected"))), orm.Set(alerts.RunID, keys[0]), orm.Set(alerts.Rule, RuleLatency), orm.Set(alerts.State, AlertStateOpen), orm.Set(alerts.Episode, Episode(1)), orm.Set(alerts.FirstAt, now), orm.Set(alerts.LastAt, now))
					return err
				case "receipt":
					return seedRetentionReceipts(t.Context(), tx, keys[0], now, retentionAt(t, now, 12*time.Hour), 1)
				case "running", "missing-state":
					runtime := Runtime{}
					if reason == "running" {
						runtime.Health.State = foundation.Running
					}
					payload, err := encodePayload(runtime, maxRuntimePayloadBytes)
					if err != nil {
						return err
					}
					_, err = orm.UpdateAll(t.Context(), tx, query, orm.Set(runs.Runtime, payload))
					return err
				case "malformed-state":
					_, err := tx.Exec(t.Context(), `UPDATE foundry_telemetry_runs SET runtime = '{"version":1,"data":{"Health":{"State":{"unknown":true}}}}' WHERE id = $1`, keys[0].String())
					return err
				}
				return nil
			})
			capture(t, s.collector)
			err := s.flush(t.Context(), nil)
			if reason == "quiet-policy" {
				if !errors.Is(err, errRunCapacity) {
					t.Fatalf("protected table = %v", err)
				}
				if countTelemetry(t, db, schema).runs != 100 {
					t.Fatal("refusal deleted rows")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertAdmissionRun(t, db, schema, keys[1], false)
			}
			assertAdmissionRun(t, db, schema, keys[0], true)
		})
	}
}

func TestPostgresTelemetryAdmissionPreservesIndependentIssuesAndEvents(t *testing.T) {
	for _, reason := range []string{"same-release-issue", "orphan-event"} {
		t.Run(reason, func(t *testing.T) {
			config := engineConfig()
			config.Retention.MaxRuns = 100
			s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
			keys := seedAdmissionRuns(t, db, schema, 100, foundation.Stopping)
			now := retentionNow(t, db, schema)
			fingerprint := Fingerprint(digest("independent-0"))
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				if err := seedRetentionIssues(t.Context(), tx, "independent", SourceBackend, now, 1); err != nil {
					return err
				}
				query := issues.Query().Where(orm.Eq(issues.Key(), fingerprint))
				if reason == "orphan-event" {
					_, err := orm.DeleteAll(t.Context(), tx, query)
					return err
				}
				_, err := orm.UpdateAll(t.Context(), tx, query, orm.Set(issues.Release, Release("development")))
				return err
			})
			reader, err := newReader(Dependencies{DB: db, Schema: schema}, config, RoutePolicy{}, &operationOwner{})
			if err != nil {
				t.Fatal(err)
			}
			issueQuery := IssueQuery{Source: SourceBackend, Page: orm.Page(1, 10)}
			eventQuery := EventQuery{Fingerprint: fingerprint, Page: orm.Page(1, 10)}
			beforeIssues, err := reader.Issues(t.Context(), issueQuery)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvents, err := reader.Events(t.Context(), eventQuery)
			if err != nil {
				t.Fatal(err)
			}
			wantIssues := int64(1)
			if reason == "orphan-event" {
				wantIssues = 0
			}
			if beforeIssues.Total != wantIssues || beforeEvents.Total != 1 {
				t.Fatal("independent issue/event fixture is incomplete")
			}
			capture(t, s.collector)
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			assertAdmissionRun(t, db, schema, keys[0], false)
			assertAdmissionRun(t, db, schema, keys[1], true)
			assertAdmissionRun(t, db, schema, s.runKey(), true)
			if got := countTelemetry(t, db, schema); got.runs != 100 || got.issues != wantIssues || got.events != 1 {
				t.Fatalf("reclaim changed independent rows: %+v", got)
			}
			afterIssues, err := reader.Issues(t.Context(), issueQuery)
			if err != nil {
				t.Fatal(err)
			}
			afterEvents, err := reader.Events(t.Context(), eventQuery)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeIssues, afterIssues) || !reflect.DeepEqual(beforeEvents, afterEvents) {
				t.Fatal("reclaim changed issue/event reader totals, identities or content")
			}
			if reason == "same-release-issue" {
				issue, err := reader.Issue(t.Context(), fingerprint)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(issue, beforeIssues.Items[0]) || issue.Release != "development" {
					t.Fatal("reclaim changed the same-release issue")
				}
			}
		})
	}
}

func TestPostgresTelemetryAdmissionBoundsAndRollback(t *testing.T) {
	for _, reason := range []string{"receipt-budget", "rollback", "kept"} {
		t.Run(reason, func(t *testing.T) {
			config := engineConfig()
			config.Retention.MaxRuns = 100
			if reason == "receipt-budget" {
				config.Retention.MaxReceiptRows = 1
			}
			s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
			count := 100
			state := foundation.Stopping
			if reason == "kept" {
				state = foundation.Running
			}
			keys := seedAdmissionRuns(t, db, schema, count, state)
			if reason == "receipt-budget" {
				now := retentionNow(t, db, schema)
				storageTransaction(t, db, schema, func(tx *database.Tx) error {
					return seedRetentionReceipts(t.Context(), tx, keys[0], retentionAt(t, now, -3*time.Hour), retentionAt(t, now, -2*time.Hour), 2)
				})
			}
			abort := errors.New("injected admission rollback")
			if reason == "rollback" {
				original := s.transact
				s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
					return original(ctx, func(tx *database.Tx) error {
						if err := fn(tx); err != nil {
							return err
						}
						return abort
					}, options...)
				}
			}
			before := countTelemetry(t, db, schema)
			b := capture(t, s.collector)
			err := s.flush(t.Context(), nil)
			want := error(errRunCapacity)
			if reason == "rollback" {
				want = abort
			}
			if !errors.Is(err, want) {
				t.Fatalf("admission = %v", err)
			}
			if after := countTelemetry(t, db, schema); after != before {
				t.Fatalf("refused transaction changed rows: %+v -> %+v", before, after)
			}
			assertAdmissionRun(t, db, schema, keys[0], true)
			if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id {
				t.Fatal("refusal advanced pending batch")
			}
		})
	}
}

func TestPostgresTelemetryAdmissionReportedTableConcurrentRecovery(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), engineConfig())
	keys := seedAdmissionRuns(t, db, schema, 1000, foundation.Stopping)
	now := retentionNow(t, db, schema)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		for i, key := range keys[892:] {
			if err := seedRetentionMetrics(t.Context(), tx, key, fmt.Sprintf("unit-%d", i), now, 60, 1); err != nil {
				return err
			}
		}
		if err := seedRetentionIssues(t.Context(), tx, "development", SourceBackend, now, 3); err != nil {
			return err
		}
		if _, err := orm.UpdateAll(t.Context(), tx, issues.Query(), orm.Set(issues.Release, Release("development"))); err != nil {
			return err
		}
		if err := seedRetentionIssues(t.Context(), tx, "orphan", SourceBackend, now, 1); err != nil {
			return err
		}
		_, err := orm.DeleteAll(t.Context(), tx, issues.Query().Where(orm.Eq(issues.Key(), Fingerprint(digest("orphan-0")))))
		return err
	})
	writers := []*storage{s}
	for range 2 {
		collector, _ := newCollector(t, collectorConfig())
		writer, err := newStorage(collector, db, schema, engineConfig())
		if err != nil {
			t.Fatal(err)
		}
		writers = append(writers, writer)
	}
	start, done := make(chan struct{}), make(chan error, 4)
	for _, writer := range writers {
		capture(t, writer.collector)
		go func() { <-start; done <- writer.flush(t.Context(), nil) }()
	}
	collector, _ := newCollector(t, collectorConfig())
	pruner, err := newStorage(collector, db, schema, engineConfig())
	if err != nil {
		t.Fatal(err)
	}
	go func() { <-start; _, err := pruner.prune(t.Context()); done <- err }()
	close(start)
	for range 4 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("admission/prune deadlock")
		}
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1000, metrics: 108, receipts: 3, issues: 3, events: 4}) {
		t.Fatalf("reported table recovery = %+v", got)
	}
	for _, writer := range writers {
		assertAdmissionRun(t, db, schema, writer.runKey(), true)
	}
	// One query checks the entire remaining legacy set, including all 108 data runs.
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		stored, err := orm.All(t.Context(), tx, runs.Query())
		if err != nil {
			return err
		}
		present := make(map[model.ID[runRow]]bool)
		for _, run := range stored {
			present[run.ID] = true
		}
		for i, key := range keys {
			if present[key] != (i >= 3) {
				t.Errorf("legacy row %d retained = %v", i, present[key])
			}
		}
		return nil
	})
}

func TestPostgresTelemetryAdmissionUnknownCommitReconciles(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint(rollback), func(t *testing.T) {
			config := engineConfig()
			config.Retention.MaxRuns = 100
			s, assertions, schema, _ := postgresStorage(t, collectorConfig(), config)
			keys := seedAdmissionRuns(t, assertions, schema, 100, foundation.Stopping)
			faultDB, armed := commitAckDatabase(t, schema, rollback)
			s.transact = faultDB.Primary().Transaction
			b := capture(t, s.collector)
			armed.Store(true)
			if err := s.flush(t.Context(), nil); !errors.Is(err, database.CommitUnknown) {
				t.Fatalf("unknown outcome = %v", err)
			}
			if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id || !s.applied.IsZero() {
				t.Fatal("unknown commit treated as success")
			}
			assertAdmissionRun(t, assertions, schema, keys[0], rollback)
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if got := countTelemetry(t, assertions, schema); got.runs != 100 || got.receipts != 1 {
				t.Fatalf("replay = %+v", got)
			}
			assertAdmissionRun(t, assertions, schema, keys[0], false)
			assertAdmissionRun(t, assertions, schema, keys[1], true)
		})
	}
}

func TestPostgresTelemetryReclaimedWriterMustPassAdmissionAgain(t *testing.T) {
	config := engineConfig()
	config.Retention.MaxRuns = 100
	s, db, schema, clock := postgresStorage(t, collectorConfig(), config)
	now := retentionNow(t, db, schema)
	clock.set(retentionAt(t, now, -6*time.Hour).UTC())
	if err := s.collector.captureRuntime(nil, Health{State: foundation.Stopping}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	keys := seedAdmissionRuns(t, db, schema, 99, foundation.Running)
	// Move this test writer's receipts into the past to model a paused process.
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.UpdateAll(t.Context(), tx, flushes.Query(), orm.Set(flushes.StoredAt, retentionAt(t, now, -3*time.Hour)), orm.Set(flushes.RetainUntil, retentionAt(t, now, -2*time.Hour)))
		return err
	})
	other, _ := newCollector(t, collectorConfig())
	newcomer, err := newStorage(other, db, schema, config)
	if err != nil {
		t.Fatal(err)
	}
	capture(t, other)
	if err := newcomer.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	assertAdmissionRun(t, db, schema, s.runKey(), false)
	clock.set(now.UTC())
	b := capture(t, s.collector)
	if err := s.flush(t.Context(), nil); !errors.Is(err, errRunCapacity) {
		t.Fatalf("missing writer bypassed admission: %v", err)
	}
	if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id {
		t.Fatal("missing writer lost pending work")
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.DeleteAll(t.Context(), tx, runs.Query().Where(orm.Eq(runs.Key(), keys[0])))
		return err
	})
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	assertAdmissionRun(t, db, schema, s.runKey(), true)
	if countTelemetry(t, db, schema).runs != 100 {
		t.Fatal("re-registration did not use freed capacity")
	}
}

func TestPostgresTelemetryRefusedEngineRecoversWithoutRestart(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	e.storage.config.Retention.MaxRuns = 100
	keys := seedAdmissionRuns(t, db, schema, 100, foundation.Running)
	var logs bytes.Buffer
	e.deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	for range 2 {
		if err := e.capture(t.Context()); err != nil {
			t.Fatal(err)
		}
		err := e.drain(t.Context())
		if !errors.Is(err, fault.Overloaded) {
			t.Fatalf("refused engine = %v", err)
		}
		e.logFailure(err)
	}
	if strings.Count(logs.String(), `"level":"WARN"`) != 1 {
		t.Fatal("repeated refusal warning")
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.DeleteAll(t.Context(), tx, runs.Query().Where(orm.Eq(runs.Key(), keys[0])))
		return err
	})
	if err := e.capture(t.Context()); err != nil {
		t.Fatal(err)
	}
	faultDB, armed := commitAckDatabase(t, schema, false)
	e.storage.transact = faultDB.Primary().Transaction
	armed.Store(true)
	if err := e.drain(t.Context()); !errors.Is(err, database.CommitUnknown) {
		t.Fatalf("unknown recovery = %v", err)
	}
	if strings.Contains(logs.String(), "telemetry run capacity recovered") {
		t.Fatal("unknown commit announced recovery")
	}
	if err := e.drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if countTelemetry(t, db, schema).receipts != 3 || len(e.collector.pending) != 0 {
		t.Fatal("same engine failed to drain refused batches")
	}
	if strings.Count(logs.String(), "telemetry run capacity recovered") != 1 {
		t.Fatal("missing recovery notice")
	}
}
