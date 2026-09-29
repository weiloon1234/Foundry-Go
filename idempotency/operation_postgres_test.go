package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type testInput struct {
	Text string `json:"text"`
}
type testOutput struct {
	Text string `json:"text"`
}

func testCodecs() (Input[testInput], Encoding[testOutput]) {
	return DefineInput("test.input.v1", func(_ context.Context, v testInput, _ int) ([]byte, error) { return json.Marshal(v) }), DefineEncoding("test.output.v1", func(_ context.Context, v testOutput, _ int) ([]byte, error) { return json.Marshal(v) }, func(_ context.Context, data []byte, _ int) (testOutput, error) {
		var v testOutput
		err := json.Unmarshal(data, &v)
		return v, err
	})
}
func testDatabase(t testing.TB) (*pgtest.Scope, *database.DB) {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	definitions := append(Migrations(), migrate.Definition{Key: migrate.Key{Origin: "test.idempotency", ID: "001_effects"}, Version: "v1", SQL: []string{`CREATE TABLE effects(id bigserial PRIMARY KEY, label text NOT NULL)`}})
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = scope.Schema()
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	return scope, db
}
func testStore(t testing.TB, db *database.DB, schema string, change func(*Config)) *Store {
	t.Helper()
	c := DefaultConfig()
	c.Schema = schema
	c.DuplicateWait = 100 * time.Millisecond
	if change != nil {
		change(&c)
	}
	store, err := New(db, keyspace.Namespace{Application: "test", Environment: "idempotency"}, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return store
}
func testOperation(t testing.TB, store *Store, id OperationID) Operation[testInput, testOutput] {
	t.Helper()
	in, out := testCodecs()
	op, err := Define(store, Definition{ID: id, Version: 1}, in, out)
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func testKey(t testing.TB, s string) Key {
	t.Helper()
	key, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func testScope(t testing.TB, caller string) Scope {
	t.Helper()
	scope, err := NewScope("test-tenant", caller)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
func effect(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
	_, err := tx.Exec(ctx, `INSERT INTO effects(label) VALUES($1)`, in.Text)
	return testOutput{Text: in.Text}, err
}
func countEffects(t testing.TB, db *database.DB, label string) int64 {
	t.Helper()
	var n int64
	if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM effects WHERE label=$1`, []any{label}, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestIndependentTransactionsClaimWaitReplayMismatch(t *testing.T) {
	scope, db := testDatabase(t)
	first := testOperation(t, testStore(t, db, scope.Schema(), nil), "submit")
	second := testOperation(t, testStore(t, scope.Open(t), scope.Schema(), nil), "submit")
	identity, key := testScope(t, "caller"), testKey(t, "concurrent-key-0001")
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		result, err := first.Run(t.Context(), identity, key, testInput{"one"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
			calls.Add(1)
			out, err := effect(ctx, tx, in)
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return testOutput{}, ctx.Err()
			}
			return out, err
		})
		if err == nil && !result.Committed() {
			err = errors.New("winner was not committed")
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("winner did not enter")
	}
	_, err := second.Run(t.Context(), identity, key, testInput{"one"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
		calls.Add(1)
		return effect(ctx, tx, in)
	})
	if !errors.Is(err, InProgress) {
		close(release)
		<-done
		t.Fatal("duplicate wait was not bounded", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result, err := second.Run(t.Context(), identity, key, testInput{"one"}, func(context.Context, *database.Tx, testInput) (testOutput, error) {
		calls.Add(1)
		return testOutput{}, errors.New("replay executed")
	})
	if err != nil || !result.Replayed() || !result.Committed() || result.Value().Text != "one" || calls.Load() != 1 || countEffects(t, db, "one") != 1 {
		t.Fatal("independent replay did not preserve outcome", err)
	}
	if _, err := second.Run(t.Context(), identity, key, testInput{"changed"}, effect); !errors.Is(err, Mismatch) {
		t.Fatal("changed input did not conflict", err)
	}
	if countEffects(t, db, "changed") != 0 {
		t.Fatal("mismatch executed")
	}
}
func TestRollbackEncodingAndPostCommitOutcomes(t *testing.T) {
	scope, db := testDatabase(t)
	op := testOperation(t, testStore(t, db, scope.Schema(), nil), "failures")
	identity := testScope(t, "caller")
	domain := errors.New("declared business rejection")
	for _, mode := range []string{"business", "panic", "goexit", "oversized", "invalid-encoding"} {
		t.Run(mode, func(t *testing.T) {
			current := op
			key := testKey(t, "failure-key-"+mode)
			if mode == "invalid-encoding" {
				current.output = DefineEncoding("broken", func(context.Context, testOutput, int) ([]byte, error) { return []byte(`{"invalid":`), nil }, func(context.Context, []byte, int) (testOutput, error) { return testOutput{}, nil })
			}
			_, err := current.Run(t.Context(), identity, key, testInput{mode}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
				out, err := effect(ctx, tx, in)
				if err != nil {
					return out, err
				}
				switch mode {
				case "business":
					return out, domain
				case "panic":
					panic("private callback")
				case "goexit":
					runtime.Goexit()
				case "oversized":
					out.Text = strings.Repeat("x", op.store.config.MaxResultBytes)
				}
				return out, nil
			})
			if err == nil || countEffects(t, db, mode) != 0 {
				t.Fatal("failed operation committed", err)
			}
			if mode == "business" && !errors.Is(err, domain) {
				t.Fatal("domain error lost")
			}
			result, err := op.Run(t.Context(), identity, key, testInput{mode}, effect)
			if err != nil || result.Replayed() || countEffects(t, db, mode) != 1 {
				t.Fatal("rollback claim prevented retry", err)
			}
		})
	}
	key := testKey(t, "after-commit-key-01")
	result, err := op.Run(t.Context(), identity, key, testInput{"after"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
		out, err := effect(ctx, tx, in)
		if err != nil {
			return out, err
		}
		return out, tx.AfterCommit(func(context.Context) error { return domain })
	})
	if !result.Committed() || !errors.Is(err, database.AfterCommitFailed) || countEffects(t, db, "after") != 1 {
		t.Fatal("known commit was discarded", err)
	}
	result, err = op.Run(t.Context(), identity, key, testInput{"after"}, effect)
	if err != nil || !result.Replayed() || countEffects(t, db, "after") != 1 {
		t.Fatal("post-commit retry executed twice", err)
	}
}
func TestCallerAdmissionNamespacesAndCancellation(t *testing.T) {
	scope, db := testDatabase(t)
	store := testStore(t, db, scope.Schema(), func(c *Config) { c.MaxRetainedPerCaller = 1 })
	op := testOperation(t, store, "admission")
	identity := testScope(t, "limited")
	key := testKey(t, "admission-key-0001")
	if _, err := op.Run(t.Context(), identity, key, testInput{"quota"}, effect); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Run(t.Context(), identity, testKey(t, "admission-key-0002"), testInput{"quota-denied"}, effect); !errors.Is(err, Capacity) {
		t.Fatal("caller quota missing", err)
	}
	if result, err := op.Run(t.Context(), identity, key, testInput{"quota"}, effect); err != nil || !result.Replayed() {
		t.Fatal("quota denied replay", err)
	}
	if _, err := op.Run(t.Context(), testScope(t, "another"), key, testInput{"other-caller"}, effect); err != nil {
		t.Fatal(err)
	}
	another := testOperation(t, testStore(t, scope.Open(t), scope.Schema(), nil), "different-operation")
	if _, err := another.Run(t.Context(), testScope(t, "another"), key, testInput{"other-operation"}, effect); err != nil {
		t.Fatal(err)
	}
	// Namespace and explicit version changes have distinct durable addresses.
	versioned := another
	versioned.definition.Version++
	if result, err := versioned.Run(t.Context(), testScope(t, "another"), key, testInput{"other-version"}, effect); err != nil || result.Replayed() {
		t.Fatal("operation version shared an address", err)
	}
	namespaced := testStore(t, scope.Open(t), scope.Schema(), nil)
	namespaced.namespace.Application += "-another"
	if result, err := testOperation(t, namespaced, "admission").Run(t.Context(), identity, key, testInput{"other-namespace"}, effect); err != nil || result.Replayed() {
		t.Fatal("application namespace shared an address", err)
	}
	otherTenant, _ := NewScope("other-tenant", "limited")
	if _, err := op.Run(t.Context(), otherTenant, key, testInput{"other-tenant"}, effect); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var called bool
	if _, err := op.Run(ctx, testScope(t, "canceled"), key, testInput{"canceled"}, func(context.Context, *database.Tx, testInput) (testOutput, error) {
		called = true
		return testOutput{}, nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatal("pre-claim cancellation executed", err)
	}
	if countEffects(t, db, "quota-denied") != 0 {
		t.Fatal("quota rejection wrote effects")
	}
}
func TestBoundedActiveWorkCancellationAndClose(t *testing.T) {
	scope, db := testDatabase(t)
	store := testStore(t, db, scope.Schema(), func(c *Config) { c.MaxActive = 1 })
	op := testOperation(t, store, "active")
	identity, key := testScope(t, "one"), testKey(t, "active-request-0001")
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := op.Run(t.Context(), identity, key, testInput{"cancel"}, func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
			if _, err := effect(ctx, tx, in); err != nil {
				return testOutput{}, err
			}
			close(entered)
			<-ctx.Done()
			return testOutput{}, ctx.Err()
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not enter")
	}
	// Exhausted local admission queues briefly, then reports a retryable
	// Unavailable outcome (HTTP 503) rather than the caller retention quota.
	bounded, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, err := op.Run(bounded, identity, key, testInput{"cancel"}, effect)
	stop()
	if !errors.Is(err, Unavailable) || !errors.Is(err, fault.Overloaded) || errors.Is(err, Capacity) {
		t.Fatal("active work unbounded", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("close did not own callback cancellation", err)
	}
	if countEffects(t, db, "cancel") != 0 || db.Stats().Owners != 0 {
		t.Fatal("canceled transaction escaped ownership")
	}
}
