// Package recursivequeries declares a natural-key hierarchy for framework acceptance.
package recursivequeries

import "github.com/weiloon1234/Foundry-Go/value"

type NodeCode string

type Node struct {
	Code   NodeCode
	Parent value.Nullable[NodeCode] `foundry:"column=parent_code"`
	Name   string
}
