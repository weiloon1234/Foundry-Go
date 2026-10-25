package observerqueries_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMObserverDeclarationsRejectForeignFamilies(t *testing.T) {
	if err := orm.Check(observerqueries.Records, observerqueries.Plains, observerqueries.Effects); err != nil {
		t.Fatal(err)
	}
	calls := 0
	declaration, err := orm.NewObserver[observerqueries.Record]("orm").Declare(func() orm.Hooks[observerqueries.Record] { calls++; return observerqueries.ORMObserverHooks("orm") })
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := lifecycle.NewObservers()
	if err != nil {
		t.Fatal(err)
	}
	if !lifecycle.HasObservers[observerqueries.Record](set) || lifecycle.HasObservers[observerqueries.Record](empty) || calls != 0 {
		t.Fatal("registration constructed a factory or leaked across instances")
	}
	foreign, err := lifecycle.NewObserver[observerqueries.Record, struct{}]("foreign").Declare(func() struct{} { calls++; return struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.NewObservers(declaration, foreign); err == nil {
		t.Fatal("one model accepted incompatible observer families")
	}
	if calls != 0 {
		t.Fatal("rejected registration constructed a hook factory")
	}
}

func TestPostgresORMObserversComposeAndRollback(t *testing.T) {
	db, stats, namespace := observerApp(t)
	var trace observerqueries.Trace
	ctx := observerqueries.WithTrace(t.Context(), &trace)
	err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		plain, err := observerqueries.Plains.Insert(ctx, tx)
		if err != nil {
			return err
		}
		if plain.Name != "registered default" || stats.Plain.Load() != 1 {
			return errors.New("unannotated model skipped observer")
		}
		item, err := observerqueries.Records.Insert(ctx, tx, observerqueries.Records.Name.Set(" SAMPLE "))
		if err != nil {
			return err
		}
		want := strings.ToLower(strings.TrimSpace(" SAMPLE |"+strings.Join(stages("saving", "creating"), "|"))) + "!"
		if item.Name != want || len(trace.ORMChanges) != 6 {
			return errors.New("shared draft, mutator or capture changed")
		}
		for _, changes := range trace.ORMChanges {
			field, err := orm.ChangeOf(changes, observerqueries.Records.Name)
			if err != nil {
				return err
			}
			if !field.Assigned() || !field.Changed() {
				return errors.New("observer lost stored changes")
			}
		}
		if stats.Retrieval.Load() != 0 {
			return errors.New("write dispatched retrieval")
		}
		if _, err = observerqueries.Records.Find(ctx, tx, item.ID); err != nil {
			return err
		}
		if stats.Retrieval.Load() != 1 {
			return errors.New("retrieval factory missing")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trace.Calls, stages("saving", "creating", "created", "saved", "committed")) || stats.First.Load() != 1 || stats.Second.Load() != 1 {
		t.Fatal("observer order or factory lifetime changed", trace.Calls)
	}
	for _, mode := range []string{"error", "panic", "goexit"} {
		err := inSchema(t, db, namespace, func(tx *database.Tx) error {
			before, err := observerqueries.Records.Query().Count(t.Context(), tx)
			if err != nil {
				return err
			}
			effects, err := observerqueries.Effects.Query().Count(t.Context(), tx)
			if err != nil {
				return err
			}
			veto := observerqueries.Trace{FailAt: "first.created", Mode: mode}
			_, err = observerqueries.Records.Insert(observerqueries.WithTrace(t.Context(), &veto), tx)
			if err == nil || (mode == "error" && !errors.Is(err, observerqueries.Veto)) || veto.Calls[len(veto.Calls)-1] != "first.created" {
				return errors.New("observer failure lost identity or continued")
			}
			after, err := observerqueries.Records.Query().Count(t.Context(), tx)
			if err != nil {
				return err
			}
			afterEffects, err := observerqueries.Effects.Query().Count(t.Context(), tx)
			if err != nil {
				return err
			}
			if before != after || effects != afterEffects {
				return errors.New("model or effect escaped rollback")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
