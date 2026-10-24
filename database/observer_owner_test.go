package database_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type observerOwnerWrapper struct{ *database.DB }

func TestObserverOwnerLookupRetainsConcreteOwnership(t *testing.T) {
	db := new(database.DB)
	if _, known := database.FoundryObservers(db, sqlowner.Observers()); known {
		t.Fatal("unbound pool proved observer absence")
	}
	if err := db.BindObservers(lifecycle.Observers{}); err != nil {
		t.Fatal(err)
	}
	primary := db.Primary()
	for _, owner := range []any{db, primary, &primary, new(database.Session), new(database.Tx)} {
		if _, known := database.FoundryObservers(owner, sqlowner.Observers()); !known {
			t.Fatalf("concrete owner %T lost its snapshot", owner)
		}
		if _, known := database.FoundryObservers(owner, sqlowner.ObserverSeal{}); known {
			t.Fatalf("zero capability opened owner %T", owner)
		}
	}
	for _, owner := range []any{nil, (*database.DB)(nil), (*database.PrimaryExecutor)(nil), database.PrimaryExecutor{}, (*database.Session)(nil), (*database.Tx)(nil), observerOwnerWrapper{db}, &observerOwnerWrapper{db}} {
		if _, known := database.FoundryObservers(owner, sqlowner.Observers()); known {
			t.Fatalf("untrusted or empty owner %T proved observer absence", owner)
		}
	}
}
