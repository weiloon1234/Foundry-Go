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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func observerApp(t *testing.T) (*database.DB, *observerqueries.Stats, string) {
	return observerAppWith(t, false)
}
func observerORMApp(t *testing.T) (*database.DB, *observerqueries.Stats, string) {
	return observerAppWith(t, true)
}
func observerAppWith(t *testing.T, useORM bool) (*database.DB, *observerqueries.Stats, string) {
	t.Helper()
	pool := foundation.NewKey[*database.DB]("observer.pool")
	statsKey := foundation.NewKey[*observerqueries.Stats]("observer.stats")
	stats := &observerqueries.Stats{}
	app := testkit.Start(t, foundry.New().Register(
		foundation.Module{Name: "second", Requires: []foundation.ProviderID{"first"}, OnRegister: func(r *foundation.Registrar) error {

			return orm.RegisterObserver(r, pool, orm.NewObserver[observerqueries.Record]("second"), func(s foundation.Resolver) (func() orm.Hooks[observerqueries.Record], error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() orm.Hooks[observerqueries.Record] {
					stats.Second.Add(1)
					return observerqueries.ORMObserverHooks("second")
				}, nil
			})

		}},
		postgres.Module("database", pool, pgtest.Config(t)),
		foundation.Module{Name: "first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, statsKey, stats); err != nil {
				return err
			}

			if err := orm.RegisterObserver(r, pool, orm.NewObserver[observerqueries.Record]("first"), func(s foundation.Resolver) (func() orm.Hooks[observerqueries.Record], error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() orm.Hooks[observerqueries.Record] {
					stats.First.Add(1)
					return observerqueries.ORMObserverHooks("first")
				}, nil
			}); err != nil {
				return err
			}
			if err := orm.RegisterObserver(r, pool, orm.NewObserver[observerqueries.Plain]("plain"), func(s foundation.Resolver) (func() orm.Hooks[observerqueries.Plain], error) {
				stats, err := foundation.Resolve(s, statsKey)
				if err != nil {
					return nil, err
				}
				return func() orm.Hooks[observerqueries.Plain] {
					stats.Plain.Add(1)
					return orm.Hooks[observerqueries.Plain]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[observerqueries.Plain]) error {
						return orm.WriteSet(draft, observerqueries.Plains.Name.Set("registered default"))
					}}
				}, nil
			}); err != nil {
				return err
			}
			if err := orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[observerqueries.Record]("record.read"), retrievalConstructor[orm.RetrievalHooks[observerqueries.Record]](statsKey)); err != nil {
				return err
			}
			return orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[observerqueries.Effect]("effect.read"), retrievalConstructor[orm.RetrievalHooks[observerqueries.Effect]](statsKey))

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
	var first observerqueries.Record
	q := observerqueries.Records
	input := []orm.Assignment[observerqueries.Record]{observerqueries.Records.Name.Set(" SAMPLE ")}
	err := inSchema(t, wrapped{db}, namespace, func(tx *database.Tx) error {
		if _, err := q.Query().All(t.Context(), tx); err != nil {
			return err
		}
		if stats.First.Load() != 0 || stats.Second.Load() != 0 {
			t.Fatal("read instantiated write observers")
		}
		item, err := q.Insert(observerqueries.WithTrace(t.Context(), &create), wrapped{tx}, input...)
		if err != nil {
			return err
		}
		first = item
		want := strings.ToLower(strings.TrimSpace(" SAMPLE |"+strings.Join(stages("saving", "creating"), "|"))) + "!"
		if item.Name != want || strings.Count(item.Name, "!") != 1 {
			t.Fatalf("draft sharing or once-only mutator: %q", item.Name)
		}
		if !reflect.DeepEqual(create.Calls, stages("saving", "creating", "created", "saved")) {
			t.Fatal("create stage order", create.Calls)
		}
		if len(create.ORMChanges) != 6 {
			t.Fatal("after callbacks skipped")
		}
		for _, changes := range create.ORMChanges {
			field, err := orm.ChangeOf(changes, observerqueries.Records.Name)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(changes, create.ORMChanges[0]) || !field.Assigned() || !field.Changed() {
				t.Fatal("observers received inconsistent change capture")
			}
		}
		item, err = q.Update(observerqueries.WithTrace(t.Context(), &update), tx, item.ID)
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
		if _, err := q.Update(observerqueries.WithTrace(t.Context(), &missing), wrapped{tx}, item.ID); !errors.Is(err, database.NotFound) || len(missing.Calls) != 0 || stats.First.Load() != 3 || stats.Second.Load() != 3 {
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
	if err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		inspected := false
		inspection := observerqueries.Trace{Inspect: func(w *orm.Write[observerqueries.Record]) error {
			name, ok := orm.WriteGet(w, q.Name.Set).Get()
			if !ok || name != " SAMPLE " || orm.WriteIsSet(w, q.Key().Set) {
				return errors.New("observer changed original values or omission")
			}
			inspected = true
			return nil
		}}
		again, err := observerqueries.Records.Insert(observerqueries.WithTrace(t.Context(), &inspection), tx, input...)
		if err != nil {
			return err
		}
		want := strings.ToLower(strings.TrimSpace(" SAMPLE |"+strings.Join(stages("saving", "creating"), "|"))) + "!"
		if !inspected || again.Name != want || again.ID == first.ID {
			t.Fatal("callback mutated the reusable input or assigned its generated identity")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRegisteredObserversOnUnannotatedModelsAndBulkSkipping(t *testing.T) {
	db, stats, namespace := observerApp(t)
	q := observerqueries.Records
	err := db.Session(t.Context(), func(session *database.Session) error {
		return inSchema(t, wrapped{session}, namespace, func(tx *database.Tx) error {
			item, err := observerqueries.Plains.Insert(t.Context(), wrapped{tx})
			if err != nil {
				return err
			}
			if item.Name != "registered default" || stats.Plain.Load() != 1 {
				t.Fatal("unannotated model skipped registered observer")
			}
			batch, err := q.InsertMany(t.Context(), tx, [][]orm.Assignment[observerqueries.Record]{{observerqueries.Records.Name.Set(" BULK ")}})
			if err != nil {
				return err
			}
			if batch[0].Name != "bulk!" {
				t.Fatal("bulk skipped field mutator")
			}
			f := observerqueries.Records
			if _, err := orm.Upsert(t.Context(), wrapped{tx}, q.Query(), orm.OnConflict(orm.ConflictKey(f.Key())).DoNothing(), observerqueries.Records.Key().Set(batch[0].ID), observerqueries.Records.Name.Set(" UPSERT ")); err != nil {
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
		_, err := observerqueries.Records.Insert(observerqueries.WithTrace(ctx, &trace), wrapped{tx})
		if !errors.Is(err, context.Canceled) || trace.Calls[len(trace.Calls)-1] != "first.created" {
			t.Fatal("cancellation did not stop callbacks", err, trace.Calls)
		}
		if n, err := observerqueries.Records.Query().Count(t.Context(), tx); err != nil || n != 0 {
			t.Fatal("canceled model write persisted", n, err)
		}
		if n, err := observerqueries.Effects.Query().Count(t.Context(), tx); err != nil || n != 0 {
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
	q, logs := observerqueries.Records, observerqueries.Effects
	err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		for _, stage := range []string{"first.saving", "first.creating", "first.created", "first.saved"} {
			for _, mode := range []string{"error", "panic", "goexit"} {
				trace := observerqueries.Trace{FailAt: stage, Mode: mode}
				if _, err := q.Insert(observerqueries.WithTrace(t.Context(), &trace), wrapped{tx}); err == nil {
					t.Fatal("observer failure accepted")
				}
				if trace.Calls[len(trace.Calls)-1] != stage {
					t.Fatal("callbacks continued after veto", trace.Calls)
				}
				if n, err := logs.Query().Count(t.Context(), tx); err != nil || n != 0 {
					t.Fatal("related observer writes escaped rollback", n, err)
				}
				if n, err := q.Query().Count(t.Context(), tx); err != nil || n != 0 {
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
			if _, err := q.Insert(observerqueries.WithTrace(t.Context(), &trace), child); err != nil {
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
		_, err := observerqueries.Records.Insert(observerqueries.WithTrace(t.Context(), &trace), tx)
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
		n, err := observerqueries.Records.Query().Count(t.Context(), tx)
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
				_, err := observerqueries.Records.Insert(observerqueries.WithTrace(t.Context(), &trace), tx)
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
