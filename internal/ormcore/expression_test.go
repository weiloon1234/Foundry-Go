package ormcore

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestExplicitReferenceFreeProvenance(t *testing.T) {
	owner := new(int)
	source := Source{Owners: map[string]any{"things": owner}}
	if CheckReferenceSet(source, nil, false) == nil {
		t.Fatal("missing ownership accepted")
	}
	if CheckReferenceSet(Source{}, nil, true) == nil {
		t.Fatal("constant without declared source accepted")
	}
	if err := CheckReferenceSet(source, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := CheckReferenceSet(source, []Reference{{Table: "things", Owner: owner}}, false); err != nil {
		t.Fatal(err)
	}
	if CheckReferenceSet(source, []Reference{{Table: "things", Owner: new(int)}}, true) == nil {
		t.Fatal("reference-free marker concealed foreign owner")
	}
	p := CompareExpressions(Expression{Node: querycore.ParameterNode{}, ReferenceFree: true}, Expression{Node: querycore.FieldRef{Table: "things", Column: "id"}, Refs: []Reference{{Table: "things", Owner: owner}}}, querycore.Equal)
	if p.ReferenceFree || len(p.Refs) != 1 {
		t.Fatal(p)
	}
	if CompareExpressions(Expression{}, Expression{}, querycore.Equal).Err == nil {
		t.Fatal("zero comparison accepted")
	}
}
