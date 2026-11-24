// Package recursivequeries declares a natural-key hierarchy for framework acceptance.
package recursivequeries

import "github.com/weiloon1234/Foundry-Go/value"

type NodeCode string

//foundry:model table=nodes var=Nodes key=Code identity=false
type Node struct {
	Code   NodeCode                 `foundry:"member=Code,key-role=primary-text"`
	Parent value.Nullable[NodeCode] `foundry:"column=parent_code"`
	Name   string
}
