package querycore

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestStatementShapeRechecksExecutionScope(t *testing.T) {
	active := statementmeta.Enable(t.Context())
	var c Compiler
	for _, ctx := range []context.Context{active, context.Background(), active, nil} {
		c.BeginStatement(ctx)
		want := ctx != nil && statementmeta.Active(ctx)
		if (c.fragments != nil) != want {
			t.Fatal("fragment state disagrees with execution scope")
		}
		text, err := c.Parameter(int64(7))
		if err != nil {
			t.Fatal(err)
		}
		s := c.Statement("SELECT " + text)
		if (s.Shape != "") != want {
			t.Fatal("reused compiler retained stale shape state", s.Shape)
		}
	}
}

func TestRelationBatchScopeAndStatementEquivalence(t *testing.T) {
	model := ModelDefinition{Table: "records", Primary: "id", Columns: []Column{{Name: "id"}}}
	q := QueryCore{Table: "records", Model: &model}
	node := RelationThroughSelect(q, q, FieldRef{Table: "records", Column: "id"}, FieldRef{Table: "records", Column: "id"}, nil, RelationTargetAlias, RelationPivotAlias)
	node.Limit = value.Set(10)
	aggregate, err := RelationAggregateSelect(node, "records", RelationTargetAlias, FieldRef{Table: RelationPivotAlias, Column: "id"}, AggregateNode{Kind: CountAll}, false, FieldRef{}, []driver.Value{int64(1)}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []SelectNode{node, aggregate} {
		active := statementmeta.Enable(t.Context())
		for _, ctx := range []context.Context{active, context.Background(), active, nil} {
			c := Compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
			sql, err := c.CompileSelect(node)
			if err != nil {
				t.Fatal(err)
			}
			s, err := CompileRead(node, ctx, false, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if s.SQLText != sql || !reflect.DeepEqual(s.Bindings, c.Arguments) {
				t.Fatal("scope tracking changed SQL or bindings")
			}
			if (s.Shape != "") != (ctx != nil && statementmeta.Active(ctx)) {
				t.Fatal("relation statement retained stale shape state")
			}
		}
	}
}
