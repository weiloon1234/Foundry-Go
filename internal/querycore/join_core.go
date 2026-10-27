package querycore

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type JoinedNode struct {
	Node    SelectNode
	Aliases map[string]reflect.Type
	Err     error
}

func JoinNodes(leftNode, rightNode SelectNode, leftAliases, rightAliases map[string]reflect.Type, leftError, rightError error, on Expression, kind JoinKind) JoinedNode {
	var result JoinedNode
	result.Err = leftError
	if result.Err == nil {
		result.Err = rightError
	}
	if result.Err != nil {
		return result
	}
	aliases := make(map[string]reflect.Type, len(leftAliases)+len(rightAliases))
	identities := make(map[reflect.Type]bool)
	for _, side := range []map[string]reflect.Type{leftAliases, rightAliases} {
		for name, identity := range side {
			if aliases[name] != nil || identities[identity] {
				result.Err = safeerror.NewFault(fault.Invalid, "join repeats an alias name or typed alias identity")
				return result
			}
			aliases[name], identities[identity] = identity, true
		}
	}
	result.Node = leftNode
	result.Node.Joins = append(slices.Clone(leftNode.Joins), JoinNode{Source: rightNode.Source, On: on, Kind: kind})
	result.Aliases = aliases
	return result
}
