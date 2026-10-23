package query

import (
	"context"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestInactiveRelationBatchCompileAllocations(t *testing.T) {
	r := testThrough()
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	keys := id.In(1, 2, 3).expression
	for _, ctx := range []context.Context{nil, context.Background()} {
		// The low-level SELECT compiler is the control: no tracking is installed.
		control := func() (Statement, error) {
			node := r.throughSelect()
			node.Predicates = append(node.Predicates, requalify(keys, throughPivotAlias))
			node.Limit = value.Set(10)
			c := compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
			sql, err := c.CompileSelect(node)
			return Statement{sql: sql, arguments: c.Arguments}, err
		}
		var got Statement
		var err error
		ordinary := testing.AllocsPerRun(100, func() {
			got, err = control()
			if err != nil {
				panic(err)
			}
		})
		expected := got
		inactive := testing.AllocsPerRun(100, func() {
			got, err = r.compileThrough(ctx, keys, 10)
			if err != nil {
				panic(err)
			}
		})
		if inactive != ordinary || got.shape != "" || !reflect.DeepEqual(got, expected) {
			t.Fatalf("ctx=%v: inactive %.0f, ordinary %.0f; equal=%v, shape=%q", ctx, inactive, ordinary, reflect.DeepEqual(got, expected), got.shape)
		}
		t.Logf("ctx=%v: inactive=control=%.0f allocations", ctx, inactive)
	}
	active := statementmeta.Enable(t.Context())
	for _, ctx := range []context.Context{active, context.Background(), active, nil} {
		through, err := r.compileThrough(ctx, keys, 10)
		if err != nil {
			t.Fatal(err)
		}
		aggregate, err := compileRelationAggregate(ctx, r.aggregateInput(), Count[cursorRecord]().node, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		want := ctx != nil && statementmeta.Active(ctx)
		if (through.shape != "") != want || (aggregate.shape != "") != want {
			t.Fatal("relation compilation did not re-evaluate scope")
		}
	}
}

func TestCompileInOverridesStoredDiagnosticScope(t *testing.T) {
	active := statementmeta.Enable(t.Context())
	q := cursorQuery().WithScopeContext(active)
	for _, ctx := range []context.Context{context.Background(), active, context.Background()} {
		s, err := q.compileIn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if (s.shape != "") != statementmeta.Active(ctx) {
			t.Fatal("stored plan scope overrode execution scope")
		}
	}
}
