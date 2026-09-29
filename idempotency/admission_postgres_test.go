package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func expireOutcomes(t testing.TB, db *database.DB, operation string) {
	t.Helper()
	// Isolated test schema: move completed outcomes of one operation into the past.
	if _, err := db.Exec(t.Context(), `UPDATE foundry_idempotency SET completed_at = now() - interval '3 hours', expires_at = now() - interval '2 hours' WHERE operation = $1 AND completed_at IS NOT NULL`, operation); err != nil {
		t.Fatal(err)
	}
}
func countOutcomes(t testing.TB, db *database.DB, namespace, operation string) int64 {
	t.Helper()
	var n int64
	if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM foundry_idempotency WHERE namespace = $1 AND operation = $2`, []any{namespace, operation}, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// One caller's new operations must not serialize on each other: a second key
// completes while the first callback still holds its transaction open.
func TestCallerOperationsDoNotSerialize(t *testing.T) {
	scope, db := testDatabase(t)
	op := testOperation(t, testStore(t, db, scope.Schema(), nil), "parallel")
	identity := testScope(t, "busy-caller")
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := op.Run(t.Context(), identity, testKey(t, "parallel-key-00001"), testInput{"first"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
			out, err := effect(ctx, tx, in)
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return testOutput{}, ctx.Err()
			}
			return out, err
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first operation did not enter")
	}
	// DuplicateWait is 100ms; the old caller-wide lock turned this into InProgress.
	started := time.Now()
	result, err := op.Run(t.Context(), identity, testKey(t, "parallel-key-00002"), testInput{"second"}, effect)
	close(release)
	if err != nil || !result.Committed() || result.Replayed() {
		t.Fatal("caller operations serialized", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatal("second operation waited for the first", elapsed)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if countEffects(t, db, "first") != 1 || countEffects(t, db, "second") != 1 {
		t.Fatal("parallel effects missing")
	}
}

// Only the claim observes DuplicateWait. An application's own lock wait is not
// an in-progress duplicate, and its own lock timeout is a retryable failure.
func TestApplicationLockWaitsAreNotInProgress(t *testing.T) {
	scope, db := testDatabase(t)
	op := testOperation(t, testStore(t, db, scope.Schema(), nil), "locks")
	identity := testScope(t, "locking-caller")
	if _, err := db.Exec(t.Context(), `INSERT INTO effects(label) VALUES('held-row')`); err != nil {
		t.Fatal(err)
	}
	hold := func(duration time.Duration, release <-chan struct{}) (<-chan struct{}, <-chan error) {
		locked := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- scope.Open(t).Transaction(t.Context(), func(tx *database.Tx) error {
				if _, err := tx.Exec(t.Context(), `SELECT id FROM effects WHERE label = 'held-row' FOR UPDATE`); err != nil {
					return err
				}
				close(locked)
				select {
				case <-time.After(duration):
				case <-release:
				}
				return nil
			})
		}()
		return locked, finished
	}
	locked, finished := hold(400*time.Millisecond, nil)
	<-locked
	result, err := op.Run(t.Context(), identity, testKey(t, "lock-wait-key-00001"), testInput{"waited"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
		if _, err := tx.Exec(ctx, `UPDATE effects SET label = label WHERE label = 'held-row'`); err != nil {
			return testOutput{}, err
		}
		return effect(ctx, tx, in)
	})
	if err != nil || !result.Committed() {
		t.Fatal("application lock wait was bounded by the duplicate wait", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	locked, finished = hold(time.Minute, release)
	<-locked
	_, err = op.Run(t.Context(), identity, testKey(t, "lock-wait-key-00002"), testInput{"timed-out"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '50ms'`); err != nil {
			return testOutput{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE effects SET label = label WHERE label = 'held-row'`); err != nil {
			return testOutput{}, err
		}
		return effect(ctx, tx, in)
	})
	close(release)
	if !errors.Is(err, Unavailable) || errors.Is(err, InProgress) || !errors.Is(err, database.LockNotAvailable) {
		t.Fatal("application lock timeout reported an in-progress duplicate", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	// The rolled-back claim allows the same key to execute on retry.
	if result, err := op.Run(t.Context(), identity, testKey(t, "lock-wait-key-00002"), testInput{"timed-out"}, effect); err != nil || result.Replayed() || countEffects(t, db, "timed-out") != 1 {
		t.Fatal("retry after application lock timeout did not execute", err)
	}
}

// Expired outcomes still replay until pruned, but no longer consume quota.
func TestCallerQuotaCountsOnlyUnexpiredOutcomes(t *testing.T) {
	scope, db := testDatabase(t)
	op := testOperation(t, testStore(t, db, scope.Schema(), func(c *Config) { c.MaxRetainedPerCaller = 1 }), "quota-expiry")
	identity := testScope(t, "quota-caller")
	first := testKey(t, "quota-expiry-key-01")
	if _, err := op.Run(t.Context(), identity, first, testInput{"q1"}, effect); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Run(t.Context(), identity, testKey(t, "quota-expiry-key-02"), testInput{"q2"}, effect); !errors.Is(err, Capacity) {
		t.Fatal("unexpired quota not enforced", err)
	}
	expireOutcomes(t, db, "quota-expiry")
	if result, err := op.Run(t.Context(), identity, testKey(t, "quota-expiry-key-02"), testInput{"q2"}, effect); err != nil || result.Replayed() {
		t.Fatal("expired outcome still consumed quota", err)
	}
	if _, err := op.Run(t.Context(), identity, testKey(t, "quota-expiry-key-03"), testInput{"q3"}, effect); !errors.Is(err, Capacity) {
		t.Fatal("new unexpired outcome did not consume quota", err)
	}
	if result, err := op.Run(t.Context(), identity, first, testInput{"q1"}, effect); err != nil || !result.Replayed() || countEffects(t, db, "q1") != 1 {
		t.Fatal("expired outcome was not replayed before pruning", err)
	}
	if countEffects(t, db, "q3") != 0 {
		t.Fatal("quota rejection wrote effects")
	}
}

// A changed result contract replays compatible stored outcomes and keeps an
// incompatible one unavailable, never re-executing committed work.
func TestResultContractEvolution(t *testing.T) {
	scope, db := testDatabase(t)
	store := testStore(t, db, scope.Schema(), nil)
	op := testOperation(t, store, "evolution")
	identity, key := testScope(t, "evolving"), testKey(t, "evolution-key-0001")
	if _, err := op.Run(t.Context(), identity, key, testInput{"evolved"}, effect); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	counted := func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
		calls.Add(1)
		return effect(ctx, tx, in)
	}
	compatible := op
	compatible.output = DefineEncoding("test.output.v2", func(_ context.Context, v testOutput, _ int) ([]byte, error) { return json.Marshal(v) }, func(_ context.Context, data []byte, _ int) (testOutput, error) {
		var v testOutput
		err := json.Unmarshal(data, &v)
		return v, err
	})
	result, err := compatible.Run(t.Context(), identity, key, testInput{"evolved"}, counted)
	if err != nil || !result.Replayed() || result.Value().Text != "evolved" {
		t.Fatal("compatible contract change did not replay", err)
	}
	incompatible := op
	incompatible.output = DefineEncoding("test.output.v3", func(_ context.Context, v testOutput, _ int) ([]byte, error) { return json.Marshal(v) }, func(context.Context, []byte, int) (testOutput, error) {
		return testOutput{}, errors.New("incompatible stored representation")
	})
	if _, err := incompatible.Run(t.Context(), identity, key, testInput{"evolved"}, counted); !errors.Is(err, Unavailable) {
		t.Fatal("incompatible contract change was not unavailable", err)
	}
	if calls.Load() != 0 || countEffects(t, db, "evolved") != 1 {
		t.Fatal("contract change re-executed committed work")
	}
}

type recordingHandler struct {
	slog.Handler
	records *atomic.Int32
}

func (h recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.records.Add(1)
	return h.Handler.Handle(ctx, r)
}

// The store-owned pruner removes only this namespace's expired outcomes in
// bounded batches and stops with Close.
func TestAutomaticPruningLifecycle(t *testing.T) {
	scope, db := testDatabase(t)
	var logged atomic.Int32
	var output bytes.Buffer
	logger := slog.New(recordingHandler{Handler: slog.NewJSONHandler(&output, nil), records: &logged})
	config := DefaultConfig()
	config.Schema = scope.Schema()
	config.PruneInterval = time.Second
	config.PruneBatch = 1
	namespace := Namespace{Application: "test", Environment: "pruning"}
	store, err := New(db, namespace, config, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = store.Close(ctx)
	})
	op := testOperation(t, store, "pruned")
	kept := testOperation(t, store, "retained")
	other := testStore(t, scope.Open(t), scope.Schema(), func(c *Config) { c.PruneInterval = 0 })
	foreign := testOperation(t, other, "pruned")
	identity := testScope(t, "pruning")
	for _, key := range []string{"pruned-key-000001", "pruned-key-000002", "pruned-key-000003"} {
		if _, err := op.Run(t.Context(), identity, testKey(t, key), testInput{key}, effect); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := kept.Run(t.Context(), identity, testKey(t, "retained-key-00001"), testInput{"kept"}, effect); err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Run(t.Context(), identity, testKey(t, "foreign-key-000001"), testInput{"foreign"}, effect); err != nil {
		t.Fatal(err)
	}
	expireOutcomes(t, db, "pruned")
	digest := store.namespaceDigest()
	if countOutcomes(t, db, digest, "pruned") != 3 {
		t.Fatal("fixture outcomes missing")
	}
	if err := store.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Start(t.Context()); err != nil {
		t.Fatal("repeated start failed", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for countOutcomes(t, db, digest, "pruned") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("automatic pruning did not remove expired outcomes")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if countOutcomes(t, db, digest, "retained") != 1 || countOutcomes(t, db, other.namespaceDigest(), "pruned") != 1 {
		t.Fatal("pruning removed unexpired or foreign outcomes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.Done():
	case <-ctx.Done():
		t.Fatal("pruner did not stop")
	}
	if err := store.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal("closed store restarted pruning", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal("repeated close failed", err)
	}
	if logged.Load() != 0 {
		t.Fatal("successful pruning logged a failure", output.String())
	}
	disabled := testStore(t, db, scope.Schema(), func(c *Config) { c.PruneInterval = 0 })
	if err := disabled.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := disabled.Close(ctx); err != nil {
		t.Fatal(err)
	}
	<-disabled.Done()
}

func TestRetainedOutcomeIndexReplacesCallerIndex(t *testing.T) {
	scope, db := testDatabase(t)
	rows, err := db.Query(t.Context(), `SELECT indexname FROM pg_catalog.pg_indexes WHERE schemaname = $1 AND tablename = 'foundry_idempotency'`, scope.Schema())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "foundry_idempotency_retained") || slices.Contains(names, "foundry_idempotency_caller") {
		t.Fatal("retained outcome index migration missing", names)
	}
}

// New operations and replays avoid savepoint and search_path round trips.
func TestOperationStatementBudget(t *testing.T) {
	scope, _ := testDatabase(t)
	var metrics driverMetrics
	config := scope.Config()
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Connector = acknowledgmentConnector{Connector: adapter.Connector, metrics: &metrics}
	db, err := database.Open(t.Context(), adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	op := testOperation(t, testStore(t, db, scope.Schema(), nil), "statement-budget")
	identity, key := testScope(t, "budget"), testKey(t, "statement-budget-0001")
	metrics.commands.Store(0)
	if _, err := op.Run(t.Context(), identity, key, testInput{"budget"}, effect); err != nil {
		t.Fatal(err)
	}
	// BEGIN, settings, claim (with its model-write savepoint pair), quota,
	// restore, business INSERT, schema, completion (with its savepoint pair),
	// COMMIT. The previous savepoint/search_path scopes issued 22 commands.
	if n := metrics.commands.Load(); n > 13 {
		t.Fatal("new operation statement budget exceeded", n)
	}
	metrics.commands.Store(0)
	if result, err := op.Run(t.Context(), identity, key, testInput{"budget"}, effect); err != nil || !result.Replayed() {
		t.Fatal(err)
	}
	// BEGIN, settings, claim (with its savepoint pair), fresh read, COMMIT.
	// The previous scopes issued 12 commands.
	if n := metrics.commands.Load(); n > 7 {
		t.Fatal("replay statement budget exceeded", n)
	}
}

// The retained-outcome index migration drops a leftover index from an earlier
// failed attempt before building, and restores the runner's lock timeout for
// the migrations after it.
func TestRetainedOutcomeIndexMigrationIsRetryable(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	config := migrate.DefaultPostgresConfig()
	config.Schema = scope.Schema()
	up := func(definitions ...migrate.Definition) error {
		registry, err := migrate.New(definitions...)
		if err != nil {
			return err
		}
		runner, err := migrate.NewPostgres(db, registry, config)
		if err != nil {
			return err
		}
		_, err = runner.Up(t.Context())
		return err
	}
	all := Migrations()
	if err := up(all[0]); err != nil {
		t.Fatal(err)
	}
	// A leftover with the target name, as an interrupted concurrent build leaves.
	if _, err := db.Exec(t.Context(), `CREATE INDEX foundry_idempotency_retained ON "`+scope.Schema()+`".foundry_idempotency (namespace)`); err != nil {
		t.Fatal(err)
	}
	check := migrate.Definition{Key: migrate.Key{Origin: "test.idempotency", ID: "002_after_index"}, Version: "v1", Requires: []migrate.Key{{Origin: MigrationOrigin, ID: IndexRetainedOutcomes}}, SQL: []string{
		`DO $$ BEGIN IF pg_catalog.current_setting('lock_timeout') <> '10s' THEN RAISE EXCEPTION 'migration lock timeout was not restored'; END IF; END $$`,
	}}
	if err := up(append(all, check)...); err != nil {
		t.Fatal("index migration did not recover from a leftover index", err)
	}
	var definition string
	if err := database.ScanOne(t.Context(), db, `SELECT indexdef FROM pg_catalog.pg_indexes WHERE schemaname = $1 AND indexname = 'foundry_idempotency_retained'`, []any{scope.Schema()}, &definition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(definition, "expires_at") {
		t.Fatal("leftover index was kept instead of rebuilt", definition)
	}
}
