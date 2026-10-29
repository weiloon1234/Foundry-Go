package has_one

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

type Node struct {
	ID       int64
	ParentID int64
	Child    relation.One[Node]
}
type Fields struct {
	orm.Table[Node, int64]
	ParentID orm.Number[Node, int64]
	Child    orm.One[Node, Node]
}

var Nodes = orm.Define("nodes", func(b orm.Builder[Node], m *Node) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.OneOf(b, &m.Child)}
})
var Child = orm.HasOneOf(Nodes.Child, orm.KeyExpr(Nodes.Key()), orm.NumberExpr(Nodes.ParentID))

func F(ctx context.Context, db database.Executor) ([]Node, error) {
	return orm.All(ctx, db, Nodes.Query().With(Child))
}
