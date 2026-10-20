package querycore

import "testing"

func TestCorrelationQualificationPreservesSharedWindowIdentity(t *testing.T) {
	definition := &NamedWindow{Name: "shared", Definition: WindowSpec{Partitions: []ValueExpression{FieldRef{Table: "records", Column: "id"}}}}
	renamer := correlationRenamer{from: "records", to: "qualified"}
	one := renamer.windowSpec(WindowSpec{Reference: definition}, 0)
	two := renamer.windowSpec(WindowSpec{Reference: definition}, 0)
	if renamer.err != nil || one.Reference != two.Reference || one.Reference == definition || one.Reference.Definition.Partitions[0].(FieldRef).Table != "qualified" || definition.Definition.Partitions[0].(FieldRef).Table != "records" {
		t.Fatal("qualification lost named identity or changed source", renamer.err)
	}
}
