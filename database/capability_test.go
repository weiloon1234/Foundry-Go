package database

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestOwnerBridgesRejectZeroCapabilityBeforeEffects(t *testing.T) {
	var zero sqlowner.AutocommitSeal
	var outcome sqlowner.OutcomeSeal
	var observer sqlowner.ObserverSeal
	// Nil receivers prove rejection happens before acquiring or marking a pool.
	var db *DB
	rows, err := db.FoundryAutocommitQuery(zero, t.Context(), "SELECT 1")
	if rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal(rows, err)
	}
	rows, err = (PrimaryExecutor{}).FoundryAutocommitQuery(zero, t.Context(), "SELECT 1")
	if rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal(rows, err)
	}
	if !errors.Is(FoundryStatementUnknown(outcome, "write", errors.New("private")), fault.Invalid) {
		t.Fatal("zero seal classified a statement")
	}
	for _, known := range []bool{func() bool { _, ok := db.FoundryObservers(observer); return ok }(), func() bool { _, ok := (*Session)(nil).FoundryObservers(observer); return ok }(), func() bool { _, ok := (*Tx)(nil).FoundryObservers(observer); return ok }()} {
		if known {
			t.Fatal("zero seal published observer ownership")
		}
	}
}
