package application_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
)

type registeredModel struct {
	ID   int64
	Name string
}
type registeredTable struct {
	orm.Table[registeredModel, int64]
	Name orm.Text[registeredModel, string]
}

func applicationTable(name string, complete bool) registeredTable {
	return orm.Define(name, func(b orm.Builder[registeredModel], m *registeredModel) registeredTable {
		t := registeredTable{Table: orm.TableOf(b, &m.ID)}
		if complete {
			t.Name = orm.TextOf(b, &m.Name)
		}
		return t
	})
}

func TestApplicationTablesFailBeforeConstructors(t *testing.T) {
	first := applicationTable("first_invalid", false)
	second := applicationTable("second_invalid", false)
	called := false
	type promoted struct{ *registeredTable }
	b := application.New(settings(), quiet(), application.WithTables(first.Table, promoted{}, second.Table)).HTTP(func(application.Services) ([]http.RouteRegistration, error) {
		called = true
		return nil, nil
	}).Register(foundation.Module{Name: "table_probe", OnRegister: func(*foundation.Registrar) error { called = true; return nil }})
	app, err := b.Build(t.Context())
	if app != nil || err == nil || called {
		t.Fatalf("invalid tables reached application construction: app=%v err=%v called=%v", app != nil, err, called)
	}
	if !errors.Is(err, fault.Panicked) {
		t.Fatal("promoted-nil failure missing from aggregate", err)
	}
	for _, name := range []string{"first_invalid", "second_invalid", "Name"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("missing declaration diagnostic %q: %v", name, err)
		}
	}
}

func TestApplicationTablesSnapshotAndIsolation(t *testing.T) {
	valid := applicationTable("registered_models", true)
	tables := []orm.CheckedTable{valid.Table}
	option := application.WithTables(tables...)
	tables[0] = nil
	for range 2 {
		app, err := application.New(settings(), quiet(), option).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		stop(t, app)
	}
	if _, err := application.New(settings(), quiet(), application.WithTables(registeredTable{})).Build(t.Context()); err == nil {
		t.Fatal("zero declaration accepted")
	}
	// A failed registration belongs to its builder, never to a global registry.
	app, err := application.New(settings(), quiet(), application.WithTables()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stop(t, app)
}

func TestApplicationTablesRejectNilAndPromotedNil(t *testing.T) {
	type promoted struct{ *registeredTable }
	var pointer *registeredTable
	for _, table := range []orm.CheckedTable{nil, pointer, promoted{}} {
		if _, err := application.New(settings(), quiet(), application.WithTables(table)).Build(t.Context()); err == nil {
			t.Fatal("nil table accepted")
		}
	}
}
