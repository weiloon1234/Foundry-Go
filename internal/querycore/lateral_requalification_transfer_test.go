package querycore

import "testing"

func TestLateralCorrelationQualificationPreservesOriginal(t *testing.T) {
	columns := []Column{{Name: "id"}}
	requirement := &ScopeRequirement{Sources: map[string][]Column{"parent": columns}}
	nested := SelectNode{Source: TableSource{Table: "records", Alias: "inner", Columns: columns},
		Predicates: []Expression{BinaryComparison{Left: FieldRef{Table: "inner", Column: "id"}, Right: FieldRef{Table: "parent", Column: "id"}, Operator: Equal}}}
	original := Subquery{Correlation: requirement, Node: SelectNode{
		Source: TableSource{Table: "records", Alias: "child", Columns: columns},
		Joins:  []JoinNode{{Kind: LeftJoin, Lateral: requirement, Source: TableSource{Alias: "lateral", Columns: columns, Query: &nested}}},
	}}
	renamed := RequalifyCorrelation(original, "qualified")
	if renamed.Err != nil {
		t.Fatal(renamed.Err)
	}
	join := renamed.Node.Joins[0]
	if join.Lateral.Sources["qualified"] == nil || join.Lateral.Sources["parent"] != nil {
		t.Fatal(join.Lateral)
	}
	if got := join.Source.Query.Predicates[0].(BinaryComparison).Right.(FieldRef).Table; got != "qualified" {
		t.Fatal(got)
	}
	if got := original.Node.Joins[0].Source.Query.Predicates[0].(BinaryComparison).Right.(FieldRef).Table; got != "parent" {
		t.Fatal("original correlation mutated", got)
	}
	if original.Node.Joins[0].Lateral.Sources["parent"] == nil {
		t.Fatal("original requirement mutated")
	}
}
