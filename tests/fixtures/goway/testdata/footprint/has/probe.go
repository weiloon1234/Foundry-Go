package has

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

type Node struct {
	ID       int64
	ParentID int64
	Children relation.Many[Node]
}
type Fields struct {
	orm.Table[Node, int64]
	ParentID orm.Number[Node, int64]
	Children orm.Many[Node, Node]
}

var Nodes = orm.Define("nodes", func(b orm.Builder[Node], m *Node) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.ManyOf(b, &m.Children)}
})
var Children = orm.HasManyOf(Nodes.Children, orm.KeyExpr(Nodes.Key()), orm.NumberExpr(Nodes.ParentID))

func F(ctx context.Context, db database.Executor) ([]Node, error) {
	return orm.All(ctx, db, Nodes.Query().Where(orm.Has(Children, orm.NumberGt(Nodes.ParentID, 0))))
}
