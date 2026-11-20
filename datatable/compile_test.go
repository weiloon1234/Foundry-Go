package datatable

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// TestCompileMatchesExecutedStatements shows Compile returns exactly the SQL and
// arguments that Query and Count send to the database for the same request.
func TestCompileMatchesExecutedStatements(t *testing.T) {
	paged := Request{Page: 2, Size: 10, Search: "Ada", Sort: []Sort{{Column: "created_at", Direction: Descending}}, Filters: []Filter{{Column: "count", Op: Greater, Values: []string{"5"}}}}
	t.Run("direct", func(t *testing.T) {
		drv := allKindsEcho()
		drv.total = 100
		assertCompileMatchesExecution(t, allKindsBenchTable("reports.compile_direct", false), drv, struct{}{}, paged)
	})
	t.Run("grouped with having", func(t *testing.T) {
		drv := &echoDriver{columns: []string{"name", "members"}, total: 1, row: func([]any) []driver.Value { return []driver.Value{"Ada", int64(6)} }}
		assertCompileMatchesExecution(t, nameTotalTable(), drv, reportActor{Tenant: 7, Allowed: true}, nameTotalRequest)
	})
	t.Run("dynamic column", func(t *testing.T) {
		request := Request{Sort: []Sort{{Column: "count", Direction: Descending}}, Filters: []Filter{{Column: "count", Op: Greater, Values: []string{"0"}}}}
		assertCompileMatchesExecution(t, localizedTable(localeConstant), allKindsEcho(), localeActor{locale: "ms"}, request)
	})
}

func assertCompileMatchesExecution[S, R, A any](t *testing.T, table Table[S, R, A], drv *echoDriver, subject A, request Request) {
	t.Helper()
	manager := echoManager(t, drv, table.Registration())
	statements, err := Compile(t.Context(), table, DefaultConfig(), subject, request)
	if err != nil {
		t.Fatal(err)
	}
	count := echoQuery{statements.Count.SQL(), statements.Count.Arguments()}
	page := echoQuery{statements.Rows.SQL(), statements.Rows.Arguments()}
	if _, err := table.Query(t.Context(), manager, subject, request); err != nil {
		t.Fatal(err)
	}
	if executed := drv.recorded(); !reflect.DeepEqual(executed, []echoQuery{count, page}) {
		t.Fatalf("Query executed:\n%#v\nCompile returned:\n%#v", executed, []echoQuery{count, page})
	}
	if _, err := table.Count(t.Context(), manager, subject, request); err != nil {
		t.Fatal(err)
	}
	if executed := drv.recorded(); !reflect.DeepEqual(executed, []echoQuery{count}) {
		t.Fatalf("Count executed:\n%#v\nCompile returned:\n%#v", executed, count)
	}
}

// TestCompileKeepsTheRequestPath shows Compile authorizes, validates the request
// and reports source failures as Query does, without reaching a database.
func TestCompileKeepsTheRequestPath(t *testing.T) {
	table := Define(reportSpec())
	if _, err := Compile(t.Context(), table, DefaultConfig(), reportActor{Tenant: 7}, Request{}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("Compile skipped authorization", err)
	}
	if _, err := Compile(t.Context(), table, DefaultConfig(), reportActor{Tenant: 7, Allowed: true}, Request{Sort: []Sort{{Column: "missing", Direction: Ascending}}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("Compile accepted an undeclared sort", err)
	}
	spec := reportSpec()
	spec.Source = func(context.Context, reportActor) (rows.SelectionSource[reportRecord], error) {
		return nil, fault.New(fault.Conflict, "source canary")
	}
	if _, err := Compile(t.Context(), Define(spec), DefaultConfig(), reportActor{Tenant: 7, Allowed: true}, Request{}); !errors.Is(err, fault.Conflict) {
		t.Fatal("Compile hid the source error", err)
	}
	if _, err := Compile(t.Context(), Table[reportRecord, ReportRow, reportActor]{}, DefaultConfig(), reportActor{Allowed: true}, Request{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("Compile accepted an undefined table", err)
	}
	broken := DefaultConfig()
	broken.MaxActive = 0
	if _, err := Compile(t.Context(), table, broken, reportActor{Tenant: 7, Allowed: true}, Request{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("Compile accepted an invalid configuration", err)
	}
	statements, err := Compile(t.Context(), table, DefaultConfig(), reportActor{Tenant: 7, Allowed: true}, Request{})
	if err != nil || statements.Rows.SQL() == "" || statements.Count.SQL() == "" {
		t.Fatal("authorized compile failed", err)
	}
}
