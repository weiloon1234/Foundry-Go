package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func zeroObservers[K any](f func(K) (lifecycle.Observers, bool)) bool {
	var k K
	_, known := f(k)
	return known
}

func zeroAutocommit[K any](f func(K, context.Context, string, ...any) (*database.Rows, error)) error {
	var k K
	_, err := f(k, context.Background(), "must not execute")
	return err
}

func zeroOutcome[K any](f func(K, string, error) error) error {
	var k K
	return f(k, "must not label unknown", errors.New("cause"))
}

func TestInferredZeroDatabaseCapabilitiesFailClosed(t *testing.T) {
	db := new(database.DB)
	if err := db.BindObservers(lifecycle.Observers{}); err != nil {
		t.Fatal(err)
	}
	if zeroObservers(db.FoundryObservers) || zeroObservers(db.Primary().FoundryObservers) || zeroObservers(new(database.Session).FoundryObservers) || zeroObservers(new(database.Tx).FoundryObservers) {
		t.Fatal("inferred zero seal proved observer absence")
	}
	// A nil DB would panic if an invalid seal reached runtime resource access.
	var absent *database.DB
	for _, err := range []error{zeroAutocommit(absent.FoundryAutocommitQuery), zeroAutocommit(db.Primary().FoundryAutocommitQuery), zeroAutocommit((database.PrimaryExecutor{}).FoundryAutocommitQuery), zeroOutcome(database.FoundryStatementUnknown)} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid capability did not fail closed", err)
		}
	}
}
