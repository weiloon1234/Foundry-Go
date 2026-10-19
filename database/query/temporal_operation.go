package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func temporalOperationSpec(op scalarOperation) (operationSpec, bool) {
	return querycore.TemporalOperationSpec(op)
}

func temporalOperationSQL(n operationNode, args []string) (string, error) {
	return querycore.TemporalOperationSQL(n, args)
}
