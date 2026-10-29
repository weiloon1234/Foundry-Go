package orm

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestRelatedValueCapabilitiesAndEmbeddedSources(t *testing.T) {
	table := aggregateModels()
	relation := HasManyOf(table.Children, KeyExpr(table.Key()), NumberExpr(table.ParentID))
	e := ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountAll}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[aggregateModel]()}}, ReferenceFree: true}
	c := ormcore.Column[int64]{Scan: func(v *int64) any { return codec.Signed[int64]().Scan(v) }}
	type embedded struct {
		AggregateSource[aggregateModel, aggregateModel]
	}
	type concrete struct {
		HasMany[aggregateModel, aggregateModel]
	}
	for _, source := range []AggregateSource[aggregateModel, aggregateModel]{nil, embedded{}, embedded{relation}, concrete{relation}} {
		if got := FoundryRelatedValue(source, e, c, sqlowner.Expressions()); !errors.Is(got.Err, fault.Invalid) {
			t.Fatal("borrowed aggregate input accepted", got.Err)
		}
	}
	if got := FoundryRelatedValue(relation, e, c, sqlowner.ExpressionSeal{}); !errors.Is(got.Err, fault.Invalid) {
		t.Fatal("zero expression seal accepted", got.Err)
	}
	if got := FoundryModelRead(table.Query(), sqlowner.MetadataSeal{}); !errors.Is(got.Source.Err, fault.Invalid) {
		t.Fatal("zero metadata seal accepted", got.Source.Err)
	}
	if got := FoundryRelatedValue(relation, e, c, sqlowner.Expressions()); got.Err != nil {
		t.Fatal(got.Err)
	}
}
