package database_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestDatabaseModuleConfigurationDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{"nil adapter", "register provider diagnostic.pool: invalid: database module needs an adapter factory"},
		{"missing connector", "construct service diagnostic.pool: invalid: database adapter needs a connector"},
		{"pool bounds", "construct service diagnostic.pool: invalid: invalid database pool bounds"},
		{"connection bound", "construct service diagnostic.pool: invalid: database needs one positive combined connection limit"},
		{"combined bound", "construct service diagnostic.pool: invalid: primary pool exceeds the combined connection bound"},
		{"read adapter", "construct service diagnostic.pool: invalid: read pool requires one adapter factory and a positive combined connection bound"},
		{"read connector", "construct service diagnostic.pool: invalid: database read adapter needs a connector"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &driverState{}
			adapter := func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }
			bounds := database.DefaultPoolConfig()
			var options []database.Option
			switch test.name {
			case "nil adapter":
				adapter = nil
			case "missing connector":
				adapter = func() (database.Adapter, error) { return database.Adapter{}, nil }
			case "pool bounds":
				bounds.MaxOpen = 0
			case "connection bound":
				options = []database.Option{database.WithConnectionLimit(0)}
			case "combined bound":
				options = []database.Option{database.WithConnectionLimit(1)}
			case "read adapter":
				options = []database.Option{database.WithReadPool(nil, database.DefaultPoolConfig(), 32)}
			case "read connector":
				options = []database.Option{database.WithReadPool(func() (database.Adapter, error) { return database.Adapter{}, nil }, database.DefaultPoolConfig(), 32)}
			}
			key := foundation.NewKey[*database.DB]("diagnostic.pool")
			_, err := foundation.NewBuilder().Register(database.Module("diagnostic.pool", key, adapter, bounds, options...)).Build(t.Context())
			var original *fault.Error
			if err == nil || err.Error() != test.message || !errors.Is(err, fault.Invalid) || !errors.As(err, &original) || !errors.Is(err, original) || state.connected.Load() != 0 {
				t.Fatal("database module lost its configuration diagnostic or acquired resources")
			}
			*original = *fault.New(fault.Invalid, "private replacement diagnostic")
			if fmt.Sprintf("%v %+v %#v", err, err, err) != test.message+" "+test.message+" "+test.message {
				t.Fatal("database module diagnostic was mutable")
			}
		})
	}
}

func TestDatabaseModuleAdapterBoundaryDiagnostics(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned=%t/goexit=%t", owned, exit), func(t *testing.T) {
				input, raw, cause := databaseDiagnosticInput(t, owned, exit)
				key := foundation.NewKey[*database.DB]("diagnostic.pool")
				module := database.Module("diagnostic.pool", key, func() (database.Adapter, error) { return database.Adapter{}, input }, database.DefaultPoolConfig())
				_, err := foundation.NewBuilder().Register(module).Build(t.Context())
				want := "construct service diagnostic.pool failed"
				if owned {
					want = "construct service diagnostic.pool: " + ownedDatabaseDiagnostic
				}
				assertDatabaseDiagnostic(t, err, input, raw, cause, want)
			})
		}
	}
}
