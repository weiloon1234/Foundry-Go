package recursivequeries

import "github.com/weiloon1234/Foundry-Go/database/orm"

type NodeColumns struct {
	orm.Table[Node, NodeCode]
	Code   orm.TextPrimary[Node, NodeCode]
	Parent orm.NullableText[Node, NodeCode]
	Name   orm.Text[Node, string]
}

var Nodes = orm.Define("nodes", func(b orm.Builder[Node], n *Node) NodeColumns {
	table := orm.TableOf(b, &n.Code)
	return NodeColumns{Table: table, Code: orm.TextKey(orm.PrimaryOf(b, table.Key())), Parent: orm.NullableTextOf(b, &n.Parent), Name: orm.TextOf(b, &n.Name)}
})
