package ormcore

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestReferenceSetDeduplicatesAndOwnsStorage(t *testing.T) {
	owner := new(int)
	a := Reference{Table: "a", Owner: owner}
	b := Reference{Table: "b", Owner: owner}
	original := []Reference{a, a}
	got, err := MergeReferences(original, []Reference{b, a, b})
	if err != nil || !reflect.DeepEqual(got, []Reference{a, b}) {
		t.Fatal(got, err)
	}
	original[0] = b
	if got[0] != a {
		t.Fatal("reference storage borrowed")
	}
	scope := reflect.TypeFor[struct{ ID int }]()
	ref := Reference{Scope: scope}
	if CheckReferences(Source{Scope: scope}, []Reference{ref}) == nil {
		t.Fatal("scope-only identity accepted without declaration")
	}
	source := Source{Scope: scope, Owners: map[string]any{"a": owner}}
	if err := CheckReferences(source, []Reference{ref}); err != nil {
		t.Fatal(err)
	}
	source.Scope = reflect.TypeFor[struct{ Name string }]()
	if CheckReferences(source, []Reference{ref}) == nil {
		t.Fatal("foreign scope accepted")
	}
}
func TestReferenceSetBoundsWorkAndRejectsInvalidIdentities(t *testing.T) {
	owner := new(int)
	input := make([]Reference, querycore.MaxExpressionNodes+1)
	for i := range input {
		input[i] = Reference{Table: "same", Owner: owner}
	}
	got, err := MergeReferences(input)
	if err == nil || len(got) != 1 {
		t.Fatal(len(got), err)
	}
	for _, owner := range []any{[]int{1}, struct{ Value any }{[]int{1}}} {
		if _, err := MergeReferences([]Reference{{Owner: owner}}); err == nil {
			t.Fatal("non-comparable identity accepted")
		}
	}
}
