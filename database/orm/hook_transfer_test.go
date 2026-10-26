package orm

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func TestModelHookFactoryRunsInsideWriteAndAfterShapeValidation(t *testing.T) {
	factories, before, begins := 0, 0, 0
	active := false
	veto := errors.New("before veto")
	d := hookedFields(func() Hooks[hookedRow] {
		factories++
		if !active {
			t.Error("factory outside transaction")
		}
		return Hooks[hookedRow]{Saving: func(_ context.Context, _ *database.Tx, original value.Optional[hookedRow], _ *Write[hookedRow]) error {
			before++
			if original.IsSet() {
				t.Error("create received original")
			}
			return veto
		}}
	})
	if _, e := Compile(d.Query()); e != nil {
		t.Fatal(e)
	}
	if factories != 0 {
		t.Fatal("read compiled hook factory")
	}
	db := hookDB(t, testConnector{onBegin: func() { begins++; active = true }})
	if _, e := d.Insert(t.Context(), db); !errors.Is(e, veto) {
		t.Fatal("missing field rejected before hook", e)
	}
	active = false
	foreign := hookedFields(nil)
	for _, input := range [][]Assignment[hookedRow]{{d.Name.Set("a"), d.Name.Set("b")}, {foreign.Name.Set("x")}} {
		if _, e := d.Insert(t.Context(), db, input...); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
	}
	if factories != 1 || before != 1 || begins != 1 {
		t.Fatal(factories, before, begins)
	}
	if e := Check(hookedFields(nil)); e != nil {
		t.Fatal("another declaration mutated", e)
	}
}
func TestWriterObserversFrameworkOwnerForms(t *testing.T) {
	db := new(database.DB)
	if e := db.BindObservers(lifecycle.Observers{}); e != nil {
		t.Fatal(e)
	}
	primary := db.Primary()
	for _, test := range []struct {
		name  string
		owner any
		known bool
	}{
		{"pool", db, true}, {"primary", primary, true}, {"primary pointer", &primary, true}, {"session", new(database.Session), true}, {"transaction", new(database.Tx), true},
		{"nil pool", (*database.DB)(nil), false}, {"nil primary", (*database.PrimaryExecutor)(nil), false}, {"zero primary", database.PrimaryExecutor{}, false}, {"zero primary pointer", new(database.PrimaryExecutor), false}, {"nil session", (*database.Session)(nil), false}, {"nil transaction", (*database.Tx)(nil), false}, {"unstarted pool", new(database.DB), false}, {"wrapper", &noEachWriter{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, known := ownerObservers(test.owner); known != test.known {
				t.Fatal(known, test.known)
			}
		})
	}
}

func TestObserverAdapterMustBeValidAndUnstartedPoolIsNotFrozen(t *testing.T) {
	if _, err := NewObserver[hookedRow]("native.adapter").Declare(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil observer factory accepted", err)
	}
	for _, owner := range []any{new(database.DB), &noEachWriter{}} {
		if _, known := ownerObservers(owner); known {
			t.Fatal("unfrozen/foreign owner claimed observer absence")
		}
	}
}
