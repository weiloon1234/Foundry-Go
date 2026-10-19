package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func jsonOperationSpec(op scalarOperation) (operationSpec, bool) {
	return querycore.JsonOperationSpec(op)
}

func jsonOperationSQL(n operationNode, args []string) (string, error) {
	return querycore.JsonOperationSQL(n, args)
}
