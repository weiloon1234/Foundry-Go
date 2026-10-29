package relation_with

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

type Parent struct{ ID int64 }
type Child struct {
	ID       int64
	ParentID int64
	Parent   relation.One[Parent]
}
type ParentFields struct{ orm.Table[Parent, int64] }
type ChildFields struct {
	orm.Table[Child, int64]
	ParentID orm.Number[Child, int64]
	Parent   orm.One[Child, Parent]
}

var Parents = orm.Define("parents", func(b orm.Builder[Parent], m *Parent) ParentFields { return ParentFields{orm.TableOf(b, &m.ID)} })
var Children = orm.Define("children", func(b orm.Builder[Child], m *Child) ChildFields {
	return ChildFields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.OneOf(b, &m.Parent)}
})
var ParentRelation = orm.BelongsToOf(Children.Parent, orm.NumberExpr(Children.ParentID), orm.KeyExpr(Parents.Key()))

func F(ctx context.Context, db database.Executor) ([]Child, error) {
	return orm.All(ctx, db, Children.Query().With(ParentRelation))
}
