package database_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestDatabaseClockDefaultsAndInvalidOptions(t *testing.T) {
	state := &driverState{}
	adapter := database.Adapter{Connector: connector{state}}
	for _, options := range [][]database.Option{{nil}, {database.WithClock(nil)}} {
		if db, err := database.Prepare(adapter, database.DefaultPoolConfig(), options...); db != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid clock option did not fail before construction", err)
		}
	}
	db, err := database.Prepare(adapter, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	if _, ok := db.Clock().(clock.System); !ok {
		t.Fatal("direct pool lost its default clock")
	}
	if state.connected.Load() != 0 {
		t.Fatal("clock configuration opened a connection")
	}
}

func TestDatabaseClockFollowsActualSessionTransactionAndSavepoint(t *testing.T) {
	initial := time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)
	source := testkit.NewClock(initial)
	state := &driverState{}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), database.WithClock(source))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	check := func(tx *database.Tx) error {
		if tx.Clock() != source || !tx.Clock().Now().Equal(source.Now()) {
			return errors.New("transaction lost its configured clock")
		}
		return tx.Savepoint(t.Context(), func(child *database.Tx) error {
			source.Advance(time.Second)
			if child.Clock() != source || !child.Clock().Now().Equal(source.Now()) {
				return errors.New("savepoint captured a stale instant or another clock")
			}
			return nil
		})
	}
	if err := db.Transaction(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(s *database.Session) error {
		if s.Clock() != source {
			return errors.New("session lost its database clock")
		}
		return s.Transaction(t.Context(), check)
	}); err != nil {
		t.Fatal(err)
	}
	if !source.Now().Equal(initial.Add(2 * time.Second)) {
		t.Fatal("clock source was replaced or normalized during ownership propagation")
	}
}

func TestDatabaseModuleInheritsEachApplicationClockAndRetainsExplicitOverride(t *testing.T) {
	state := &driverState{}
	key := foundation.NewKey[*database.DB]("test.clock.database")
	adapter := func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }
	module := database.Module("test.clock.database", key, adapter, database.DefaultPoolConfig())
	for year := 2030; year < 2032; year++ {
		source := testkit.NewClock(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC))
		app, err := foundry.New(foundation.WithClock(source)).Register(module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		db, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Start(t.Context()); !errors.Is(err, database.NotReady) {
			t.Fatal("module started before clock binding", err)
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if db.Clock() != source {
			t.Fatal("module shared a clock across applications")
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	explicit := testkit.NewClock(time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC))
	ambient := testkit.NewClock(time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC))
	options := []database.Option{database.WithClock(explicit)}
	overridden := database.Module("test.clock.database", key, adapter, database.DefaultPoolConfig(), options...)
	options[0] = database.WithClock(ambient)
	app, err := foundry.New(foundation.WithClock(ambient)).Register(overridden).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(t.Context())
	db, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if db.Clock() != explicit {
		t.Fatal("module replaced its explicit clock or retained caller-owned option slice")
	}
}
