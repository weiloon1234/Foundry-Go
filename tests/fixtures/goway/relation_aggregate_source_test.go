package goway_test

import (
	"errors"
	"testing"

	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type fakeAggregateSource struct {
	orm.AggregateSource[j.Parent, j.Target]
}
type promotedAggregateSource struct {
	orm.HasMany[j.Parent, j.Target]
}
type promotedAggregateMethod promotedAggregateSource

func TestRelationAggregateSourceProvenance(t *testing.T) {
	var nilNative *orm.HasMany[j.Parent, j.Target]
	var nilFake *fakeAggregateSource
	for name, source := range map[string]orm.AggregateSource[j.Parent, j.Target]{
		"nil":                         nil,
		"nil_native":                  nilNative,
		"zero_native":                 orm.HasMany[j.Parent, j.Target]{},
		"nil_wrapper":                 nilFake,
		"empty_interface_wrapper":     fakeAggregateSource{},
		"populated_interface_wrapper": fakeAggregateSource{j.DirectTargets},
		"pointer_interface_wrapper":   &fakeAggregateSource{j.DirectTargets},
		"empty_promoted_method":       promotedAggregateMethod{},
		"populated_promoted_method":   promotedAggregateMethod{j.DirectTargets},
	} {
		t.Run(name, func(t *testing.T) {
			slot := j.Parents.Count.Using(expr.Related(source, expr.Count[j.Target]()))
			if err := orm.Check(slot); !errors.Is(err, fault.Invalid) {
				t.Fatalf("source accepted: %v", err)
			}
			g := &goldenCapture{reply: aggregateReply}
			db := aggregateDB(t, g)
			if _, err := j.Parents.Query().With(slot).All(t.Context(), db); !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid query: %v", err)
			}
			if g.statements != 0 {
				t.Fatalf("invalid source executed %d statements", g.statements)
			}
		})
	}
	for _, source := range []orm.AggregateSource[j.Parent, j.Target]{j.DirectTargets, &j.DirectTargets, j.Peers} {
		if err := orm.Check(j.Parents.Count.Using(expr.Related(source, expr.Count[j.Target]()))); err != nil {
			t.Fatal(err)
		}
	}
	if err := orm.Check(j.ParentAggregates.Pivots); err != nil {
		t.Fatal(err)
	}
}
