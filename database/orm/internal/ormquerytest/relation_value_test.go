package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

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
	relation := orm.HasManyOf(table.Children, orm.KeyExpr(table.Key()), orm.NumberExpr(table.ParentID))
	e := ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountAll}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[aggregateModel]()}}, ReferenceFree: true}
	c := ormcore.Column[int64]{Scan: func(v *int64) any { return codec.Signed[int64]().Scan(v) }}
	type embedded struct {
		orm.AggregateSource[aggregateModel, aggregateModel]
	}
	type concrete struct {
		orm.HasMany[aggregateModel, aggregateModel]
	}
	for _, source := range []orm.AggregateSource[aggregateModel, aggregateModel]{nil, embedded{}, embedded{relation}, concrete{relation}} {
		if got := orm.FoundryRelatedValue(source, e, c, sqlowner.Expressions()); !errors.Is(got.Err, fault.Invalid) {
			t.Fatal("borrowed aggregate input accepted", got.Err)
		}
	}
	if got := orm.FoundryRelatedValue(relation, e, c, sqlowner.ExpressionSeal{}); !errors.Is(got.Err, fault.Invalid) {
		t.Fatal("zero expression seal accepted", got.Err)
	}
	if got := orm.FoundryModelRead(table.Query(), sqlowner.MetadataSeal{}); !errors.Is(got.Source.Err, fault.Invalid) {
		t.Fatal("zero metadata seal accepted", got.Source.Err)
	}
	if got := orm.FoundryRelatedValue(relation, e, c, sqlowner.Expressions()); got.Err != nil {
		t.Fatal(got.Err)
	}
}
