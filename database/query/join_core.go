package query

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type joinedNode = querycore.JoinedNode

func joinNodes(leftNode, rightNode selectNode, leftAliases, rightAliases map[string]reflect.Type, leftError, rightError error, on expression, kind joinKind) joinedNode {
	return querycore.JoinNodes(leftNode, rightNode, leftAliases, rightAliases, leftError, rightError, on, kind)
}
