package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"strings"
	"testing"
)

type planExecutor struct {
	calls     int
	sql       string
	arguments []any
	failure   error
}

func (p *planExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	panic("EXPLAIN must query its plan result")
}

func (p *planExecutor) Query(_ context.Context, sql string, args ...any) (*database.Rows, error) {
	p.calls++
	p.sql = sql
	p.arguments = args
	return nil, p.failure
}

func TestExplainRetainsBindingsFailuresAndTransactionCapability(t *testing.T) {
	sentinel := errors.New("selected executor failed")
	executor := &planExecutor{failure: sentinel}
	fields := records()
	query := fields.Query().Where(orm.KeyEq(fields.ID, 42))
	compiled, err := orm.Compile(query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Explain(t.Context(), executor, query); !errors.Is(err, sentinel) {
		t.Fatal("executor failure lost", err)
	}
	if executor.calls != 1 || executor.sql != "EXPLAIN (FORMAT JSON, ANALYZE FALSE, VERBOSE FALSE, COSTS TRUE) "+compiled.SQL() || !reflect.DeepEqual(executor.arguments, compiled.Arguments()) {
		t.Fatal("compiled statement or bindings changed")
	}
	if _, err := orm.ExplainAnalyze(t.Context(), executor, query); !errors.Is(err, sentinel) || !strings.Contains(executor.sql, "ANALYZE TRUE") {
		t.Fatal("analysis mode not explicit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := orm.Explain(ctx, executor, query); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := orm.ExplainLocked(t.Context(), nil, orm.Lock(query)); !errors.Is(err, fault.Invalid) {
		t.Fatal("locked plan accepted no transaction", err)
	}
	if _, err := orm.Explain(t.Context(), executor, orm.Query[record]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid query reached executor", err)
	}
	if executor.calls != 2 {
		t.Fatal("invalid plan invoked SQL")
	}
}
