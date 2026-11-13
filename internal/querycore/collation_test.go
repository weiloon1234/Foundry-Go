package querycore

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDistinctSkipsOnlyDefaultCollation(t *testing.T) {
	c := NewCompiler([]Column{{Name: "name"}})
	c.Sources = map[string]map[string]Column{"names": c.Columns}
	field := FieldRef{Table: "names", Column: "name"}
	plan, err := c.PlanValue(field)
	if err != nil {
		t.Fatal(err)
	}
	var distinct DistinctOrder
	distinct.Add(plan, 1)
	node := OperationNode{Kind: CollateOperation, Result: codec.TypeText, Collation: "C", DefaultCollation: true, Arguments: []OperationArgument{{Value: field, Kind: codec.TypeText}}}
	if sql, err := distinct.Compile(&c, OrderNode{Expression: node}, nil, false); err != nil || sql != "1" {
		t.Fatal("DISTINCT rejected a default collation", sql, err)
	}
	node.DefaultCollation = false
	if _, err := distinct.Compile(&c, OrderNode{Expression: node}, nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("DISTINCT silently removed explicit collation", err)
	}
	plan, err = c.PlanValue(node)
	if err != nil {
		t.Fatal(err)
	}
	distinct.Add(plan, 2)
	if sql, err := distinct.Compile(&c, OrderNode{Expression: node}, nil, false); err != nil || sql != "2" {
		t.Fatal("DISTINCT lost matching explicit collation", sql, err)
	}
	node.DefaultCollation, node.Collation = true, "bad\x00name"
	if _, err := distinct.Compile(&c, OrderNode{Expression: node}, nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("DISTINCT bypassed collation validation", err)
	}
}

func TestCollationValidation(t *testing.T) {
	operand := OperationArgument{Kind: codec.TypeText, Value: FieldRef{Table: "names", Column: "name"}}
	for _, name := range []string{"", strings.Repeat("a", 64), strings.Repeat("é", 32), "bad\x00name", "bad\nname", "bad\x7fname", "bad\u0085name", "bad\xffname"} {
		node := OperationNode{Kind: CollateOperation, Result: codec.TypeText, Collation: name, Arguments: []OperationArgument{operand}}
		if err := node.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid collation accepted: %q: %v", name, err)
		}
	}
	for _, name := range []string{"C", "und-x-icu", strings.Repeat("a", 63), strings.Repeat("é", 31) + "a", `a"b`, "app.collation"} {
		node := OperationNode{Kind: CollateOperation, Result: codec.TypeText, Collation: name, Arguments: []OperationArgument{operand}}
		if err := node.Validate(); err != nil {
			t.Fatalf("valid collation rejected: %q: %v", name, err)
		}
	}
	for _, node := range []OperationNode{
		{Kind: CollateOperation, Result: codec.TypeText, Collation: "C"},
		{Kind: CollateOperation, Result: codec.TypeText, Collation: "C", Arguments: []OperationArgument{operand, operand}},
		{Kind: CollateOperation, Result: codec.TypeInteger, Collation: "C", Arguments: []OperationArgument{operand}},
		{Kind: CollateOperation, Result: codec.TypeText, Collation: "C", Arguments: []OperationArgument{{Kind: codec.TypeInteger, Value: operand.Value}}},
		{Kind: CollateOperation, Result: codec.TypeText, Collation: "C", Arguments: []OperationArgument{{Kind: codec.TypeText}}},
	} {
		if err := node.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid collation operation accepted", err)
		}
	}
}

func TestCollationQuotedIdentifierRendering(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{`a"b`, `("names"."name" COLLATE "a""b")`},
		{"app.collation", `("names"."name" COLLATE "app.collation")`},
		{`x"); SELECT 1; --`, `("names"."name" COLLATE "x""); SELECT 1; --")`},
	} {
		c := NewCompiler([]Column{{Name: "name"}})
		c.Sources = map[string]map[string]Column{"names": c.Columns}
		node := OperationNode{Kind: CollateOperation, Result: codec.TypeText, Collation: tc.name, Arguments: []OperationArgument{{Kind: codec.TypeText, Value: FieldRef{Table: "names", Column: "name"}}}}
		sql, err := c.SelectedExpression(node, nil, false)
		if err != nil || c.render(sql, false) != tc.want || len(c.Arguments) != 0 {
			t.Fatal("collation identifier rendering", c.render(sql, false), c.Arguments, err)
		}
	}
	if got := QuotedTable(`app.na"mes`); got != `"app"."na""mes"` {
		t.Fatal("qualified table quoting changed", got)
	}
}
