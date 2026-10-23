package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// K is inferred from a public method; the caller never names its internal type.
type catcher[K any] struct {
	*database.DB
	called bool
}

func (c *catcher[K]) FoundryObservers(K) (lifecycle.Observers, bool) {
	c.called = true
	return lifecycle.Observers{}, true
}
func observerCatcher[K any](_ func(K) (lifecycle.Observers, bool), db *database.DB) *catcher[K] {
	return &catcher[K]{DB: db}
}

type embeddedObserverOwner struct{ *database.DB }

func TestObserverCapabilitiesNeverReachApplicationImplementations(t *testing.T) {
	db := new(database.DB)
	if err := db.BindObservers(lifecycle.Observers{}); err != nil {
		t.Fatal(err)
	}
	captured := observerCatcher(db.FoundryObservers, db)
	q := mutatorQuery(func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil })
	for _, owner := range []database.Transactor{captured, embeddedObserverOwner{db}} {
		if _, known := writerObservers(owner); known {
			t.Fatal("unknown wrapper proved observer absence")
		}
		if err := q.setWriteLifecycle(owner, updateModel); !errors.Is(err, fault.Invalid) {
			t.Fatal("unknown wrapper bypassed model observers", err)
		}
	}
	if captured.called {
		t.Fatal("application captured observer capability")
	}
}

func TestObserverLookupPreservesFrameworkOwnerForms(t *testing.T) {
	db := new(database.DB)
	if err := db.BindObservers(lifecycle.Observers{}); err != nil {
		t.Fatal(err)
	}
	primary := db.Primary()
	enclosing := enclosingTransaction{new(database.Tx)}
	q := mutatorQuery(func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil })
	for _, tc := range []struct {
		name  string
		owner database.Transactor
	}{
		{"DB pointer", db},
		{"PrimaryExecutor value", primary},
		{"PrimaryExecutor pointer", &primary},
		{"Session pointer", new(database.Session)},
		{"Tx pointer", new(database.Tx)},
		{"enclosingTransaction value", enclosing},
		{"enclosingTransaction pointer", &enclosing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, known := writerObservers(tc.owner); !known {
				t.Fatal("concrete framework owner cannot prove observer absence")
			}
			for _, kind := range []mutationKind{updateModel, deleteModel} {
				if err := q.setWriteLifecycle(tc.owner, kind); err != nil {
					t.Fatalf("unobserved framework owner requires WithoutModelHooks for %v: %v", kind, err)
				}
			}
		})
	}
	for _, owner := range []database.Transactor{(*database.DB)(nil), (*database.PrimaryExecutor)(nil), (*database.Session)(nil), (*database.Tx)(nil), (*enclosingTransaction)(nil)} {
		if _, known := writerObservers(owner); known {
			t.Fatalf("nil owner %T proved observer absence", owner)
		}
	}
}
