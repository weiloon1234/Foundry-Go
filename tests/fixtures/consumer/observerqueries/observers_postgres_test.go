package observerqueries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"foundry.test/consumer/observerqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func observerApp(t *testing.T) (*database.DB, *observerqueries.Stats, string) {
	t.Helper()
	pool := foundation.NewKey[*database.DB]("observer.pool")
	statsKey := foundation.NewKey[*observerqueries.Stats]("observer.stats")
	stats := &observerqueries.Stats{}
	app := testkit.Start(t, foundry.New().Register(
		foundation.Module{Name: "second", Requires: []foundation.ProviderID{"first"}, OnRegister: func(r *foundation.Registrar) error {
			return observerqueries.RegisterRecordObserver(r, pool, observerqueries.NewRecordObserver("second"), func(s foundation.Resolver) (func() observerqueries.RecordHooks, error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() observerqueries.RecordHooks {
					stats.Second.Add(1)
					return observerqueries.ObserverHooks("second")
				}, nil
			})
		}},
		postgres.Module("database", pool, pgtest.Config(t)),
		foundation.Module{Name: "first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, statsKey, stats); err != nil {
				return err
			}
			if err := observerqueries.RegisterRecordObserver(r, pool, observerqueries.NewRecordObserver("first"), func(s foundation.Resolver) (func() observerqueries.RecordHooks, error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() observerqueries.RecordHooks { stats.First.Add(1); return observerqueries.ObserverHooks("first") }, nil
			}); err != nil {
				return err
			}
			if err := observerqueries.RegisterPlainObserver(r, pool, observerqueries.NewPlainObserver("plain"), func(s foundation.Resolver) (func() observerqueries.PlainHooks, error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() observerqueries.PlainHooks {
					stats.Plain.Add(1)
					return observerqueries.PlainHooks{Creating: func(_ context.Context, _ *database.Tx, draft *observerqueries.PlainDraft) error {
						*draft = draft.SetName("registered default")
						return nil
					}}
				}, nil
			}); err != nil {
				return err
			}
			if err := observerqueries.RegisterRecordRetrievalObserver(r, pool, observerqueries.NewRecordRetrievalObserver("record.read"), retrievalConstructor[observerqueries.RecordRetrievalHooks](statsKey)); err != nil {
				return err
			}
			return observerqueries.RegisterEffectRetrievalObserver(r, pool, observerqueries.NewEffectRetrievalObserver("effect.read"), retrievalConstructor[observerqueries.EffectRetrievalHooks](statsKey))
		}},
	))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		for _, ddl := range []string{
			`CREATE TABLE observed_records(id uuid PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE observed_plain(id uuid PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE observed_effects(id uuid PRIMARY KEY,label text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return db, stats, namespace
}

func retrievalConstructor[H any](statsKey foundation.Key[*observerqueries.Stats]) func(foundation.Resolver) (func() H, error) {
	return func(s foundation.Resolver) (func() H, error) {
		stats, err := foundation.Resolve(s, statsKey)
		if err != nil {
			return nil, err
		}
		return func() H { stats.Retrieval.Add(1); return *new(H) }, nil
	}
}

func inSchema(t *testing.T, writer database.Transactor, namespace string, fn func(*database.Tx) error) error {
	t.Helper()
	return writer.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		return fn(tx)
	})
}

type wrapped struct{ writer database.Transactor }

func (w wrapped) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return w.writer.Transaction(ctx, fn, options...)
}
func stages(names ...string) []string {
	var result []string
	for _, stage := range names {
		for _, observer := range []string{"local", "first", "second"} {
			result = append(result, observer+"."+stage)
		}
	}
	return result
}

func TestPostgresObserversComposeByStageAndCaptureOnce(t *testing.T) {
	db, stats, namespace := observerApp(t)
	var create, update, remove observerqueries.Trace
	q := observerqueries.QueryObservedRecords()
	input := observerqueries.RecordDraft{}.SetName(" SAMPLE ")
	err := inSchema(t, wrapped{db}, namespace, func(tx *database.Tx) error {
		if _, err := q.All(t.Context(), tx); err != nil {
			return err
		}
		if stats.First.Load() != 0 || stats.Second.Load() != 0 {
			t.Fatal("read instantiated write observers")
		}
		item, err := q.Create(observerqueries.WithTrace(t.Context(), &create), wrapped{tx}, input)
		if err != nil {
			return err
		}
		want := strings.ToLower(strings.TrimSpace(" SAMPLE |"+strings.Join(stages("saving", "creating"), "|"))) + "!"
		if item.Name != want || strings.Count(item.Name, "!") != 1 {
			t.Fatalf("draft sharing or once-only mutator: %q", item.Name)
		}
		if !reflect.DeepEqual(create.Calls, stages("saving", "creating", "created", "saved")) {
			t.Fatal("create stage order", create.Calls)
		}
		if len(create.Changes) != 6 {
			t.Fatal("after callbacks skipped")
		}
		for _, changes := range create.Changes {
			if !reflect.DeepEqual(changes, create.Changes[0]) || !changes.Fields().Name.Assigned() || !changes.Fields().Name.Changed() {
				t.Fatal("observers received inconsistent change capture")
			}
		}
		item, err = q.Update(observerqueries.WithTrace(t.Context(), &update), tx, item.ID, observerqueries.RecordDraft{})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(update.Calls, stages("saving", "updating", "updated", "saved")) {
			t.Fatal("update stage order", update.Calls)
		}
		if _, err := q.Delete(observerqueries.WithTrace(t.Context(), &remove), wrapped{tx}, item.ID); err != nil {
			return err
		}
		if !reflect.DeepEqual(remove.Calls, stages("deleting", "deleted")) {
			t.Fatal("delete stage order", remove.Calls)
		}
		var missing observerqueries.Trace
		if _, err := q.Update(observerqueries.WithTrace(t.Context(), &missing), wrapped{tx}, item.ID, observerqueries.RecordDraft{}); !errors.Is(err, database.NotFound) || len(missing.Calls) != 0 || stats.First.Load() != 3 || stats.Second.Load() != 3 {
			t.Fatal("missing model instantiated observers", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, trace := range []*observerqueries.Trace{&create, &update, &remove} {
		if !reflect.DeepEqual(trace.Calls[len(trace.Calls)-3:], stages("committed")) {
			t.Fatal("outer commit order", trace.Calls)
		}
	}
	if stats.First.Load() != 3 || stats.Second.Load() != 3 {
		t.Fatal("factory was not once per normal operation")
	}
	if stats.Retrieval.Load() != 0 {
		t.Fatal("normal writes or observer side effects constructed retrieval hooks")
	}
	if name, _ := input.Name().Get(); name != " SAMPLE " || input.ID().IsSet() {
		t.Fatal("callback mutated input draft")
	}
}

func TestPostgresRegisteredObserversOnUnannotatedModelsAndBulkSkipping(t *testing.T) {
	db, stats, namespace := observerApp(t)
	q := observerqueries.QueryObservedRecords()
	err := db.Session(t.Context(), func(session *database.Session) error {
		return inSchema(t, wrapped{session}, namespace, func(tx *database.Tx) error {
			item, err := observerqueries.QueryObservedPlain().Create(t.Context(), wrapped{tx}, observerqueries.PlainDraft{})
			if err != nil {
				return err
			}
			if item.Name != "registered default" || stats.Plain.Load() != 1 {
				t.Fatal("unannotated model skipped registered observer")
			}
			batch, err := q.CreateMany(t.Context(), tx, []observerqueries.RecordDraft{observerqueries.RecordDraft{}.SetName(" BULK ")})
			if err != nil {
				return err
			}
			if batch[0].Name != "bulk!" {
				t.Fatal("bulk skipped field mutator")
			}
			f := observerqueries.RecordFields()
			if _, err := q.Upsert(t.Context(), wrapped{tx}, observerqueries.RecordDraft{}.SetID(batch[0].ID).SetName(" UPSERT "), query.OnConflict[observerqueries.Record](f.ID).DoNothing()); err != nil {
				return err
			}
			if stats.First.Load() != 0 || stats.Second.Load() != 0 {
				t.Fatal("bulk/upsert instantiated observers")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresObserverCancellationRollsBackAndStopsLaterStages(t *testing.T) {
	db, _, namespace := observerApp(t)
	err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		trace := observerqueries.Trace{FailAt: "first.created", Mode: "cancel", Cancel: cancel}
		_, err := observerqueries.QueryObservedRecords().Create(observerqueries.WithTrace(ctx, &trace), wrapped{tx}, observerqueries.RecordDraft{})
		if !errors.Is(err, context.Canceled) || trace.Calls[len(trace.Calls)-1] != "first.created" {
			t.Fatal("cancellation did not stop callbacks", err, trace.Calls)
		}
		if n, err := observerqueries.QueryObservedRecords().Count(t.Context(), tx); err != nil || n != 0 {
			t.Fatal("canceled model write persisted", n, err)
		}
		if n, err := observerqueries.QueryObservedEffects().Count(t.Context(), tx); err != nil || n != 0 {
			t.Fatal("canceled observer effect persisted", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresObserverVetoAndSavepointRollback(t *testing.T) {
	db, _, namespace := observerApp(t)
	q, logs := observerqueries.QueryObservedRecords(), observerqueries.QueryObservedEffects()
	err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		for _, stage := range []string{"first.saving", "first.creating", "first.created", "first.saved"} {
			for _, mode := range []string{"error", "panic", "goexit"} {
				trace := observerqueries.Trace{FailAt: stage, Mode: mode}
				if _, err := q.Create(observerqueries.WithTrace(t.Context(), &trace), wrapped{tx}, observerqueries.RecordDraft{}); err == nil {
					t.Fatal("observer failure accepted")
				}
				if trace.Calls[len(trace.Calls)-1] != stage {
					t.Fatal("callbacks continued after veto", trace.Calls)
				}
				if n, err := logs.Count(t.Context(), tx); err != nil || n != 0 {
					t.Fatal("related observer writes escaped rollback", n, err)
				}
				if n, err := q.Count(t.Context(), tx); err != nil || n != 0 {
					t.Fatal("model write escaped rollback", n, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var trace observerqueries.Trace
	err = inSchema(t, db, namespace, func(tx *database.Tx) error {
		return tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if _, err := q.Create(observerqueries.WithTrace(t.Context(), &trace), child, observerqueries.RecordDraft{}); err != nil {
				return err
			}
			return observerqueries.Veto
		})
	})
	if !errors.Is(err, observerqueries.Veto) {
		t.Fatal(err)
	}
	for _, call := range trace.Calls {
		if strings.HasSuffix(call, ".committed") {
			t.Fatal("rolled-back parent dispatched after commit")
		}
	}
}

func TestPostgresObserversAfterCommitFailureKeepsLaterCallbacks(t *testing.T) {
	db, _, namespace := observerApp(t)
	trace := observerqueries.Trace{FailAt: "first.committed"}
	err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		_, err := observerqueries.QueryObservedRecords().Create(observerqueries.WithTrace(t.Context(), &trace), tx, observerqueries.RecordDraft{})
		return err
	})
	var outcome *database.Error
	if !errors.As(err, &outcome) || outcome.Outcome() != database.Committed || !errors.Is(err, observerqueries.Veto) {
		t.Fatal("after commit outcome lost", err)
	}
	if !reflect.DeepEqual(trace.Calls[len(trace.Calls)-3:], stages("committed")) {
		t.Fatal("later after-commit observer was skipped", trace.Calls)
	}
	if err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		n, err := observerqueries.QueryObservedRecords().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatal("committed record rolled back")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresObserverFactoriesAreIndependentAcrossConcurrentWrites(t *testing.T) {
	db, stats, namespace := observerApp(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			trace := observerqueries.Trace{}
			err := inSchema(t, db, namespace, func(tx *database.Tx) error {
				_, err := observerqueries.QueryObservedRecords().Create(observerqueries.WithTrace(t.Context(), &trace), tx, observerqueries.RecordDraft{})
				return err
			})
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(trace.Calls, stages("saving", "creating", "created", "saved", "committed")) {
				t.Error("operation-local callback state leaked", trace.Calls)
			}
		})
	}
	wg.Wait()
	if stats.First.Load() != 8 || stats.Second.Load() != 8 {
		t.Fatal("observer factory invocation count changed")
	}
}
