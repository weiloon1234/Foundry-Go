package nested_with

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

type Node struct {
	ID       int64
	ParentID int64
	Parent   relation.One[Node]
}
type Fields struct {
	orm.Table[Node, int64]
	ParentID orm.Number[Node, int64]
	Parent   orm.One[Node, Node]
}

var Nodes = orm.Define("nodes", func(b orm.Builder[Node], m *Node) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.OneOf(b, &m.Parent)}
})
var Parent = orm.BelongsToOf(Nodes.Parent, orm.NumberExpr(Nodes.ParentID), orm.KeyExpr(Nodes.Key()))

func F(ctx context.Context, db database.Executor) ([]Node, error) {
	return orm.All(ctx, db, Nodes.Query().With(Parent.With(Parent).Where(orm.NumberGt(Nodes.ParentID, 0)).OrderBy(orm.KeyAsc(Nodes.Key()))))
}
