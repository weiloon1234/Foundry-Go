//go:build !race

package querycore

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Allocation equality excludes race instrumentation and its goroutine bookkeeping.
// Scope reuse and statement equivalence also run under race in statement_shape_scope_test.go.
func TestInactiveCompileAllocations(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "name"}}
	assignment := MutationAssignment{Field: FieldRef{Table: "records", Column: "name"}, AssignmentValue: AssignmentValue{Bind: func() (driver.Value, error) { return "private", nil }}}
	assignments := []MutationAssignment{assignment}
	assigned := map[string]MutationAssignment{"name": assignment}
	base := QueryCore{Table: "records", Model: &ModelDefinition{Table: "records", Primary: "id", Columns: columns}}
	for _, ctx := range []context.Context{nil, context.Background()} {
		q := base
		q.ScopeContext = ctx
		for _, mutation := range []bool{false, true} {
			// Control uses the existing low-level compiler without diagnostic tracking.
			// These envelopes are the pre-detector read/update construction steps.
			control := func() (Statement, error) {
				if !mutation {
					c := Compiler{ScopeContext: ctx}
					sql, err := c.CompileSelect(q.ModelSelect(nil))
					return Statement{SQLText: sql, Bindings: c.Arguments}, err
				}
				c, err := q.WriteCompiler(UpdateModel, false, false)
				if err != nil {
					return Statement{}, err
				}
				prefix, err := c.CompileCTEs(q.ModelSelect(nil))
				if err != nil {
					return Statement{}, err
				}
				sets := make([]string, 0, len(assigned))
				for _, a := range assignments {
					p, err := c.AssignmentParameter(a.Field.Column, a.Bind)
					if err != nil {
						return Statement{}, err
					}
					sets = append(sets, Quoted(a.Field.Column)+" = "+p)
				}
				var sql strings.Builder
				sql.WriteString(prefix)
				sql.WriteString("UPDATE " + QuotedTable(q.Table) + " SET " + strings.Join(sets, ", "))
				if err := c.Where(&sql, q.EffectivePredicates()); err != nil {
					return Statement{}, err
				}
				sql.WriteString(" RETURNING " + SelectedColumns(q.Table, q.Model.Columns))
				return Statement{SQLText: sql.String(), Bindings: c.Arguments}, nil
			}
			compile := func() (Statement, error) {
				if !mutation {
					return q.CompileModel(ReadModels, nil)
				}
				c, err := q.WriteCompiler(UpdateModel, false, false)
				if err != nil {
					return Statement{}, err
				}
				return q.CompileMutation(c, q.ModelSelect(nil), UpdateModel, assignments, assigned, nil, false)
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
				got, err = compile()
				if err != nil {
					panic(err)
				}
			})
			if inactive != ordinary || got.Shape != "" || !reflect.DeepEqual(got, expected) {
				t.Fatalf("mutation=%v ctx=%v: inactive %.0f, ordinary %.0f; statements equal=%v, shape=%q", mutation, ctx, inactive, ordinary, reflect.DeepEqual(got, expected), got.Shape)
			}
			t.Logf("mutation=%v ctx=%v: inactive=control=%.0f allocations", mutation, ctx, inactive)
		}
	}
}

func TestInactiveRelationBatchCompileAllocations(t *testing.T) {
	model := ModelDefinition{Table: "records", Primary: "id", Columns: []Column{{Name: "id"}}}
	q := QueryCore{Table: "records", Model: &model}
	node := RelationThroughSelect(q, q, FieldRef{Table: "records", Column: "id"}, FieldRef{Table: "records", Column: "id"}, nil, RelationTargetAlias, RelationPivotAlias)
	node.Limit = value.Set(10)
	aggregate, err := RelationAggregateSelect(node, "records", RelationTargetAlias, FieldRef{Table: RelationPivotAlias, Column: "id"}, AggregateNode{Kind: CountAll}, false, FieldRef{}, []driver.Value{int64(1)}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []SelectNode{node, aggregate} {
		for _, ctx := range []context.Context{nil, context.Background()} {
			var got Statement
			var err error
			control := testing.AllocsPerRun(100, func() {
				c := Compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
				var sql string
				sql, err = c.CompileSelect(node)
				got = Statement{SQLText: sql, Bindings: c.Arguments}
				if err != nil {
					panic(err)
				}
			})
			want := got
			inactive := testing.AllocsPerRun(100, func() {
				got, err = CompileRead(node, ctx, false, nil, true)
				if err != nil {
					panic(err)
				}
			})
			if inactive != control || got.Shape != "" || !reflect.DeepEqual(got, want) {
				t.Fatalf("inactive=%v control=%v equal=%v", inactive, control, reflect.DeepEqual(got, want))
			}
		}
		active := statementmeta.Enable(t.Context())
		for _, ctx := range []context.Context{active, context.Background(), active, nil} {
			s, err := CompileRead(node, ctx, false, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if (s.Shape != "") != (ctx != nil && statementmeta.Active(ctx)) {
				t.Fatal("relation statement retained stale shape state")
			}
		}
	}
}
