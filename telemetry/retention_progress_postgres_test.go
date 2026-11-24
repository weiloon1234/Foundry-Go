package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestPostgresTelemetryAdmissionProgressConvergesAfterLoweredCap(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	e.storage.config.Retention.MaxRuns = 100
	keys := seedAdmissionRuns(t, db, schema, 220, foundation.Stopping)
	var logs bytes.Buffer
	e.deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	b := capture(t, e.collector)
	for attempt, want := range []int64{170, 120, 100} {
		err := e.drain(t.Context())
		if attempt < 2 {
			if !errors.Is(err, errRunCapacity) {
				t.Fatalf("progress attempt %d: %v", attempt+1, err)
			}
			e.logFailure(err)
			if len(e.collector.pending) != 1 || e.collector.pending[0].id != b.id || !e.storage.applied.IsZero() {
				t.Fatal("cleanup consumed the refused batch")
			}
			if strings.Contains(logs.String(), "telemetry run capacity recovered") {
				t.Fatal("cleanup announced recovery")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if len(e.collector.pending) != 0 || e.storage.applied != b.id {
				t.Fatal("final admission did not apply the original batch")
			}
		}
		got := countTelemetry(t, db, schema)
		wantReceipts := int64(0)
		if attempt == 2 {
			wantReceipts = 1
		}
		if got.runs != want || got.receipts != wantReceipts {
			t.Fatalf("attempt %d committed rows: %+v", attempt+1, got)
		}
		assertAdmissionRun(t, db, schema, e.storage.runKey(), attempt == 2)
	}
	if strings.Count(logs.String(), `"level":"WARN"`) != 1 || strings.Count(logs.String(), "telemetry run capacity recovered") != 1 || !strings.Contains(logs.String(), `"refusals":2`) {
		t.Fatal("progress changed refusal/recovery logging")
	}
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
			if present[key] != (i >= 121) {
				t.Errorf("legacy run %d retained = %v", i, present[key])
			}
		}
		return nil
	})
}

func TestPostgresTelemetryAdmissionProgressKeepsProtectedTable(t *testing.T) {
	config := engineConfig()
	config.Retention.MaxRuns = 100
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	keys := seedAdmissionRuns(t, db, schema, 220, foundation.Running)
	b := capture(t, s.collector)
	for range 3 {
		if err := s.flush(t.Context(), nil); !errors.Is(err, errRunCapacity) {
			t.Fatalf("protected table: %v", err)
		}
		if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 220}) {
			t.Fatalf("protected table changed: %+v", got)
		}
		if len(s.collector.pending) != 1 || s.collector.pending[0].id != b.id || !s.applied.IsZero() {
			t.Fatal("refusal advanced batch")
		}
	}
	assertAdmissionRun(t, db, schema, keys[0], true)
	assertAdmissionRun(t, db, schema, s.runKey(), false)
}

func TestPostgresTelemetryAdmissionProgressBoundsAndRollback(t *testing.T) {
	for _, reason := range []string{"partial", "receipt-budget", "rollback"} {
		t.Run(reason, func(t *testing.T) {
			config := engineConfig()
			config.Retention.MaxRuns = 100
			if reason == "receipt-budget" {
				config.Retention.MaxReceiptRows = 1
			}
			s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
			keys := seedAdmissionRuns(t, db, schema, 220, foundation.Stopping)
			now := retentionNow(t, db, schema)
			storageTransaction(t, db, schema, func(tx *database.Tx) error {
				if reason == "receipt-budget" {
					return seedRetentionReceipts(t.Context(), tx, keys[0], retentionAt(t, now, -3*time.Hour), retentionAt(t, now, -2*time.Hour), 2)
				}
				if reason == "partial" {
					payload, err := encodePayload(Runtime{Health: Health{State: foundation.Running}}, maxRuntimePayloadBytes)
					if err != nil {
						return err
					}
					_, err = orm.UpdateAll(t.Context(), tx, runs.Query().Where(orm.In(runs.Key(), keys[10:]...)), orm.Set(runs.Runtime, payload))
					return err
				}
				return nil
			})
			abort := errors.New("injected progress rollback")
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
			capture(t, s.collector)
			err := s.flush(t.Context(), nil)
			wantErr := error(errRunCapacity)
			if reason == "rollback" {
				wantErr = abort
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("progress result: %v", err)
			}
			want := before
			if reason == "partial" {
				want.runs -= 10
			}
			if got := countTelemetry(t, db, schema); got != want {
				t.Fatalf("progress rows = %+v, want %+v", got, want)
			}
			assertAdmissionRun(t, db, schema, keys[0], reason != "partial")
			assertAdmissionRun(t, db, schema, keys[10], true)
			if reason == "partial" {
				if err := s.flush(t.Context(), nil); !errors.Is(err, errRunCapacity) {
					t.Fatal(err)
				}
				if got := countTelemetry(t, db, schema); got != want {
					t.Fatal("no-candidate attempt changed rows")
				}
			}
		})
	}
}

func TestPostgresTelemetryAdmissionProgressUnknownCommit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint(rollback), func(t *testing.T) {
			e, db, schema, _ := postgresEngine(t)
			e.storage.config.Retention.MaxRuns = 100
			seedAdmissionRuns(t, db, schema, 220, foundation.Stopping)
			var logs bytes.Buffer
			e.deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			b := capture(t, e.collector)
			if err := e.drain(t.Context()); !errors.Is(err, errRunCapacity) {
				t.Fatal(err)
			} else {
				e.logFailure(err)
			}
			faultDB, armed := commitAckDatabase(t, schema, rollback)
			e.storage.transact = faultDB.Primary().Transaction
			armed.Store(true)
			err := e.drain(t.Context())
			if !errors.Is(err, database.CommitUnknown) || errors.Is(err, errRunCapacity) {
				t.Fatalf("unknown cleanup reported a definite outcome: %v", err)
			}
			if len(e.collector.pending) != 1 || e.collector.pending[0].id != b.id || e.collector.uncertainBatch != b.id || !e.storage.applied.IsZero() {
				t.Fatal("unknown cleanup changed batch ownership")
			}
			if strings.Contains(logs.String(), "telemetry run capacity recovered") {
				t.Fatal("unknown cleanup announced recovery")
			}
			want := int64(120)
			if rollback {
				want = 170
			}
			if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: want}) {
				t.Fatalf("server-only outcome = %+v", got)
			}
			// A later attempt rechecks the table under the lock; it never blindly
			// repeats an uncertain deletion or counts that deletion as a batch.
			for range 2 {
				err = e.drain(t.Context())
				if err == nil {
					break
				}
				if !errors.Is(err, errRunCapacity) {
					t.Fatal(err)
				}
			}
			if err != nil {
				t.Fatal("later attempts did not converge", err)
			}
			if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 100, receipts: 1}) {
				t.Fatalf("later admission = %+v", got)
			}
			if e.storage.applied != b.id || len(e.collector.pending) != 0 || !e.collector.uncertainBatch.IsZero() {
				t.Fatal("later admission did not reconcile batch identity")
			}
			if strings.Count(logs.String(), "telemetry run capacity recovered") != 1 {
				t.Fatal("missing or repeated recovery")
			}
		})
	}
}
