package querycore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRelationDeferredScopeAvoidsAliasCapture(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "office_id"}}
	office := TableSource{Table: "offices", Columns: columns}
	desk := TableSource{Table: "desks", Columns: columns}
	scope := ScopeNode{NeedsContext: true, Resolve: func(context.Context) (Expression, error) {
		inner := DirectRelationExists(SelectNode{Source: office}, FieldRef{"desks", "office_id"}, FieldRef{"offices", "id"}, columns)
		return SubqueryPredicate{Query: inner}, nil
	}}
	outer := DirectRelationExists(SelectNode{Source: desk, Predicates: []Expression{scope}}, FieldRef{"offices", "id"}, FieldRef{"desks", "office_id"}, columns)
	node := SelectNode{Source: office, Predicates: []Expression{SubqueryPredicate{Query: outer}}}
	for range 2 {
		c := NewCompiler(nil)
		c.ScopeContext = t.Context()
		sql, err := c.SelectSQL(node)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range []string{
			`FROM "desks" AS "foundry_related"`,
			`FROM "offices" AS "foundry_related_2"`,
			`"foundry_related_2"."id" = "foundry_related"."office_id"`,
			`"foundry_related"."office_id" = "offices"."id"`,
		} {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("missing %s in %s", fragment, sql)
			}
		}
	}
}

func TestCorrelationRenameKeepsOriginalShadowingInvalid(t *testing.T) {
	columns := []Column{{Name: "id"}}
	for _, joined := range []bool{false, true} {
		node := SelectNode{Source: TableSource{Table: "records", Alias: "outer", Columns: columns}}
		if joined {
			node.Source.Alias = "other"
			node.Joins = []JoinNode{{Kind: CrossJoin, Source: TableSource{Table: "records", Alias: "outer", Columns: columns}}}
		}
		q := RequalifyCorrelation(RelationExistence(node, "outer", columns), "renamed")
		if !errors.Is(q.Err, fault.Invalid) {
			t.Fatal("original shadowing was repaired", q.Err)
		}
	}
}

func TestCorrelationRenamePreservesJoinAndNestedBindings(t *testing.T) {
	columns := []Column{{Name: "id"}}
	field := func(table string) FieldRef { return FieldRef{table, "id"} }
	inner := SelectNode{Source: TableSource{Table: "records", Alias: "leaf", Columns: columns},
		Predicates: []Expression{BinaryComparison{Left: field("leaf"), Right: field("destination"), Operator: Equal}}}
	node := SelectNode{Source: TableSource{Table: "records", Alias: "local", Columns: columns},
		Joins: []JoinNode{{Source: TableSource{Table: "records", Alias: "destination", Columns: columns},
			On: BinaryComparison{Left: field("destination"), Right: field("outer"), Operator: Equal}}},
		Predicates: []Expression{SubqueryPredicate{Query: RelationExistence(inner, "destination", columns)}},
		Locks:      []LockSpec{{Strength: LockUpdate, Targets: []string{"destination"}, Explicit: true}},
	}
	q := RequalifyCorrelation(RelationExistence(node, "outer", columns), "destination")
	c := NewCompiler(nil)
	c.AllowLocks = true
	sql, err := c.SelectSQL(SelectNode{Source: TableSource{Table: "records", Alias: "destination", Columns: columns}, Predicates: []Expression{SubqueryPredicate{Query: q}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`AS "destination_2"`, `"destination_2"."id" = "destination"."id"`, `"leaf"."id" = "destination_2"."id"`, `OF "destination_2"`} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("missing %s in %s", fragment, sql)
		}
	}
	if node.Joins[0].Source.Alias != "destination" || node.Locks[0].Targets[0] != "destination" || inner.Predicates[0].(BinaryComparison).Right != field("destination") {
		t.Fatal("requalification mutated the reusable source")
	}
}
