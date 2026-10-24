package querycore

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSelectSensitivityRecursiveOutputsAndScalar(t *testing.T) {
	columns := []Column{{Name: "a"}, {Name: "b"}, {Name: "public"}}
	anchor := SelectNode{Source: TableSource{Table: "items", Columns: columns, Sensitive: map[string]bool{"a": true}}, Selections: ColumnSelections("items", columns)}
	ref := &RecursiveReference{Name: "recursive", Columns: columns}
	step := SelectNode{Source: TableSource{Self: ref, Columns: columns}, Selections: []SelectItem{{Expression: FieldRef{Table: "recursive", Column: "b"}}, {Expression: FieldRef{Table: "recursive", Column: "a"}}, {Expression: FieldRef{Table: "recursive", Column: "public"}}}}
	cte := &CteNode{Name: "recursive", Columns: columns, Query: anchor, Recursion: &RecursiveCTE{Reference: ref, Step: step, Operator: UnionAllSet}}
	source := TableSource{Cte: cte, Alias: "renamed", Columns: columns}
	node := SelectNode{Source: source, Selections: ColumnSelections("renamed", columns)}
	flags, err := SelectSensitivity(node)
	if err != nil || !reflect.DeepEqual(flags, []bool{true, true, false}) {
		t.Fatal(flags, err)
	}
	if anchor.Source.Sensitive["b"] {
		t.Fatal("analysis mutated source metadata")
	}
	// A scalar selection can reference a sensitive enclosing output.
	inner := SelectNode{Source: TableSource{Table: "other", Columns: []Column{{Name: "id"}}}, Selections: []SelectItem{{Expression: FieldRef{Table: "renamed", Column: "b"}}}}
	node.Selections = []SelectItem{{Expression: ScalarSubquery{Query: Subquery{Node: inner}}}, {Expression: FieldRef{Table: "renamed", Column: "public"}}}
	flags, err = SelectSensitivity(node)
	if err != nil || !reflect.DeepEqual(flags, []bool{true, false}) {
		t.Fatal(flags, err)
	}
}

func TestSelectSensitivityFailsClosedAndUsesOutputPositions(t *testing.T) {
	cols := []Column{{Name: "left_name"}, {Name: "safe"}}
	left := SelectNode{Source: TableSource{Table: "left", Columns: cols}, Selections: ColumnSelections("left", cols)}
	right := SelectNode{Source: TableSource{Table: "right", Columns: []Column{{Name: "different"}, {Name: "safe"}}, Sensitive: map[string]bool{"different": true}}, Selections: ColumnSelections("right", []Column{{Name: "different"}, {Name: "safe"}})}
	for op := UnionSet; op <= ExceptAllSet; op++ {
		node := SelectNode{Source: TableSource{Set: &SetNode{Left: left, Right: right, Operator: op}, Alias: SetAlias, Columns: cols}, Selections: ColumnSelections(SetAlias, cols)}
		flags, err := SelectSensitivity(node)
		if err != nil || !reflect.DeepEqual(flags, []bool{true, false}) {
			t.Fatal(op, flags, err)
		}
	}
	invalid := left
	invalid.Selections = []SelectItem{{Expression: FieldRef{Table: "missing", Column: "id"}}}
	if _, err := SelectSensitivity(invalid); !errors.Is(err, fault.Invalid) {
		t.Fatal("unknown provenance accepted", err)
	}
	cyclic := &CteNode{Name: "cycle", Columns: cols}
	cyclic.Query = SelectNode{Source: TableSource{Cte: cyclic, Columns: cols}, Selections: ColumnSelections("cycle", cols)}
	if _, err := SelectSensitivity(cyclic.Query); !errors.Is(err, fault.Invalid) {
		t.Fatal("cyclic provenance accepted", err)
	}
	deep := left
	for i := 0; i < MaxExpressionDepth+1; i++ {
		inner := deep
		deep = SelectNode{Source: TableSource{Query: &inner, Alias: "nested", Columns: cols}, Selections: ColumnSelections("nested", cols)}
	}
	if _, err := SelectSensitivity(deep); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded provenance accepted", err)
	}
	constant := left
	constant.Selections = []SelectItem{{Expression: ParameterNode{Kind: codec.TypeText, Value: "private", Sensitive: true}}, {Expression: ParameterNode{Kind: codec.TypeText, Value: "public"}}}
	flags, err := SelectSensitivity(constant)
	if err != nil || !reflect.DeepEqual(flags, []bool{true, false}) {
		t.Fatal(flags, err)
	}
}

func TestSensitiveValueRetainsScopeAndSQLIdentity(t *testing.T) {
	columns := []Column{{Name: "id"}}
	field := FieldRef{Table: "items", Column: "id"}
	node := SelectNode{Source: TableSource{Table: "items", Columns: columns}, Selections: []SelectItem{{Expression: WithSensitivity(field, true)}}, GroupBy: []ValueExpression{field}}
	compiler := Compiler{}
	sql, err := compiler.CompileSelect(node)
	if err != nil || sql != `SELECT "items"."id" FROM "items" GROUP BY "items"."id"` {
		t.Fatal(sql, err)
	}
	flags, err := SelectSensitivity(node)
	if err != nil || !reflect.DeepEqual(flags, []bool{true}) {
		t.Fatal(flags, err)
	}
	renamed := RequalifyValue(node.Selections[0].Expression, "alias")
	if f, ok := UnwrapSensitivity(renamed).(FieldRef); !ok || f.Table != "alias" {
		t.Fatal("annotation lost requalification")
	}
	node.Source.Alias = "alias"
	node.Selections[0].Expression = renamed
	node.GroupBy = nil
	flags, err = SelectSensitivity(node)
	if err != nil || !flags[0] {
		t.Fatal("annotation lost on requalification", err)
	}
	// Marking a field never supplies missing ownership or bypasses validation.
	node.Selections[0].Expression = WithSensitivity(FieldRef{Table: "foreign", Column: "id"}, true)
	compiler = Compiler{}
	if _, err := compiler.CompileSelect(node); !errors.Is(err, fault.Invalid) {
		t.Fatal("sensitivity bypassed scope validation", err)
	}
}
