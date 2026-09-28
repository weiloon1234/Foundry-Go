package database_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSeederSelectionExpandsDependenciesAndCanRunAgain(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	var order []seed.ID
	definition := func(id seed.ID, requires ...seed.ID) seed.Definition {
		return seed.Definition{ID: id, Requires: requires, Run: func(ctx context.Context, tx *database.Tx) error {
			order = append(order, id)
			_, err := tx.Exec(ctx, "seed domain data")
			return err
		}}
	}
	input := []seed.Definition{definition("app.users", "app.roles"), definition("app.roles"), definition("app.unselected")}
	registry, err := seed.New(input...)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Requires[0] = "mutated"
	ids := registry.IDs()
	ids[0] = "mutated"
	for range 2 {
		result, err := registry.Run(t.Context(), db, "app.users")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Committed, []seed.ID{"app.roles", "app.users"}) || result.StoppedAt != nil {
			t.Fatal("selected seeder order lost")
		}
	}
	if !reflect.DeepEqual(order, []seed.ID{"app.roles", "app.users", "app.roles", "app.users"}) || state.committed.Load() != 4 {
		t.Fatal("explicit repeated seeding was suppressed or expanded incorrectly")
	}
}

func TestSeederFailuresPreserveTransactionOutcomes(t *testing.T) {
	for _, afterCommit := range []bool{false, true} {
		state := &driverState{}
		db := open(t, state, nil)
		cause := errors.New("seeder failed")
		registry, err := seed.New(
			seed.Definition{ID: "app.first", Run: func(context.Context, *database.Tx) error { return nil }},
			seed.Definition{ID: "app.second", Run: func(_ context.Context, tx *database.Tx) error {
				if afterCommit {
					return tx.AfterCommit(func(context.Context) error { return cause })
				}
				return cause
			}},
			seed.Definition{ID: "app.third", Run: func(context.Context, *database.Tx) error { t.Error("ran after failed seeder"); return nil }},
		)
		if err != nil {
			t.Fatal(err)
		}
		result, err := registry.Run(t.Context(), db)
		if !errors.Is(err, cause) || result.StoppedAt == nil || *result.StoppedAt != "app.second" {
			t.Fatalf("seeder failure result: %+v %v", result, err)
		}
		if afterCommit {
			if !reflect.DeepEqual(result.Committed, []seed.ID{"app.first", "app.second"}) || state.committed.Load() != 2 {
				t.Fatal("after-commit failure hid persisted seed")
			}
		} else {
			if !reflect.DeepEqual(result.Committed, []seed.ID{"app.first"}) || state.rolledBack.Load() != 1 {
				t.Fatal("failed seed reported committed")
			}
		}
	}
}

func TestSeederDefinitionAndSelectionValidationHappensBeforeDatabaseUse(t *testing.T) {
	run := func(context.Context, *database.Tx) error { return nil }
	for _, definitions := range [][]seed.Definition{
		{{ID: "invalid id", Run: run}}, {{ID: "app.nil"}},
		{{ID: "app.a", Run: run}, {ID: "app.a", Run: run}},
		{{ID: "app.a", Requires: []seed.ID{"app.missing"}, Run: run}},
		{{ID: "app.a", Requires: []seed.ID{"app.b"}, Run: run}, {ID: "app.b", Requires: []seed.ID{"app.a"}, Run: run}},
	} {
		if _, err := seed.New(definitions...); err == nil {
			t.Fatal("invalid seeder graph accepted")
		}
	}
	registry, err := seed.New(seed.Definition{ID: "app.a", Run: run})
	if err != nil {
		t.Fatal(err)
	}
	state := &driverState{}
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	if _, err := registry.Run(t.Context(), db, "app.missing"); !errors.Is(err, fault.Missing) {
		t.Fatal("unknown seeder reached database")
	}
	if _, err := registry.Run(t.Context(), db, "app.a", "app.a"); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate selection accepted")
	}
	if state.connected.Load() != 0 {
		t.Fatal("selection validation opened a connection")
	}
}
