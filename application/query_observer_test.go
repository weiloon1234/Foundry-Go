package application_test

import (
	"context"
	"errors"
	"fmt"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestQueryObserverOptions(t *testing.T) {
	observer := func(context.Context, database.QueryEvent) {}
	for _, opts := range [][]application.Option{
		{application.WithQueryObserver(nil)},
		{application.WithQueryObserver(observer), application.WithQueryObserver(observer)},
	} {
		if _, err := application.New(settings(), opts...).Build(t.Context()); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid observer options: %v", err)
		}
	}
}

func TestQueryObserverFrameworkPoolsAndApplicationIsolation(t *testing.T) {
	primary := infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	var mu sync.Mutex
	events := [2][]database.QueryEvent{}
	apps := [2]*application.App{}
	for i := range apps {
		s := settings()
		s.HTTP.Enabled = false
		c := infrastructure.DefaultConnectionSettings()
		c.Primary, c.Read, c.ReadEnabled = primary, primary, true
		s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": c, "other": c}
		app, err := application.New(s, quiet(), application.WithQueryObserver(func(_ context.Context, event database.QueryEvent) {
			mu.Lock()
			events[i] = append(events[i], event)
			mu.Unlock()
			// The existing pool boundary contains observer panics.
			panic("observer-private-sentinel")
		})).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, app) })
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		apps[i] = app
	}
	for i, app := range apps {
		mu.Lock()
		beforeOther := len(events[1-i])
		mu.Unlock()
		for _, name := range []database.ConnectionName{"default", "other"} {
			db, err := app.Resources().Databases.Connection(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, query := range []func(context.Context, string, ...any) (*database.Rows, error){db.Query, db.QueryRead} {
				result, err := query(t.Context(), "SELECT $1::text", "argument-private-sentinel")
				if err != nil {
					t.Fatal(err)
				}
				var got string
				if !result.Next() {
					_ = result.Close()
					t.Fatal("missing result")
				}
				if err := result.Scan(&got); err != nil {
					_ = result.Close()
					t.Fatal(err)
				}
				if err := result.Close(); err != nil {
					t.Fatal(err)
				}
				if got != "argument-private-sentinel" {
					t.Fatal("statement result changed")
				}
			}
		}
		mu.Lock()
		if len(events[1-i]) != beforeOther {
			t.Error("observer leaked across applications")
		}
		matched := map[database.PoolRole]int{}
		for _, event := range events[i] {
			if event.Statement == "SELECT $1::text" {
				matched[event.Role]++
			}
			if strings.Contains(fmt.Sprintf("%+v", event), "argument-private-sentinel") {
				t.Error("observer exposed arguments")
			}
		}
		mu.Unlock()
		if matched[database.PrimaryPool] != 2 || matched[database.ReadPool] != 2 {
			t.Fatalf("pool observations: %v", matched)
		}
	}
}

func TestQueryObserverDatabaseCommandPool(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	c := infrastructure.DefaultConnectionSettings()
	c.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": c}
	var observed atomic.Int64
	app, err := application.New(s, quiet(), application.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if e.Statement == "SELECT $1::text" {
			observed.Add(1)
		}
	})).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	seeders, err := seed.New(seed.Definition{ID: "app.observe", Run: func(ctx context.Context, tx *database.Tx) error {
		var result string
		return database.ScanOne(ctx, tx, "SELECT $1::text", []any{"private-value"}, &result)
	}})
	if err != nil {
		t.Fatal(err)
	}
	command, err := dbcommand.Parse([]string{"seed", "run"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RunDatabaseCommand(t.Context(), command, application.DatabaseCommandResources{Seeders: seeders}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if observed.Load() != 1 {
		t.Fatal("command pool did not retain observer")
	}
}
