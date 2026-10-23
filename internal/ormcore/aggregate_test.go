package ormcore

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestAggregateFiltersKeepImmutableBoundedReferences(t *testing.T) {
	ref := Reference{Table: "records", Owner: new(int)}
	field := Expression{Node: querycore.FieldRef{Table: "records", Column: "amount"}, Refs: []Reference{ref}}
	base := Aggregate(field, querycore.SumValues, querycore.ExactNumberCast)
	p := Predicate{Node: querycore.Junction{}, Refs: []Reference{ref}}
	derived := FilterAggregate(base, []Predicate{p})
	for range 64 {
		derived = FilterAggregate(derived, []Predicate{p})
		if len(derived.Refs) != 1 || derived.Err != nil {
			t.Fatal(derived)
		}
	}
	if base.Node.(querycore.AggregateNode).Filter != nil || len(derived.Node.(querycore.AggregateNode).Filter.Predicates) != 65 {
		t.Fatal("filter mutation")
	}
	invalid := FilterAggregate(base, make([]Predicate, querycore.MaxExpressionNodes+1))
	if !errors.Is(invalid.Err, fault.Invalid) {
		t.Fatal(invalid.Err)
	}
	invalid = FilterAggregate(base, []Predicate{{}})
	if !errors.Is(invalid.Err, fault.Invalid) {
		t.Fatal("zero filter accepted", invalid.Err)
	}
	if out := FilterAggregate(Expression{}, []Predicate{p}); !errors.Is(out.Err, fault.Invalid) {
		t.Fatal("zero aggregate accepted", out)
	}
}
