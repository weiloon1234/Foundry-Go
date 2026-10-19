package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func intervalOperationSpec(op scalarOperation) (operationSpec, bool) {
	return querycore.IntervalOperationSpec(op)
}

func intervalOperationSQL(n operationNode, args []string) (string, error) {
	return querycore.IntervalOperationSQL(n, args)
}
