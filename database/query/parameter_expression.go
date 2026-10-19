package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type parameterNode = querycore.ParameterNode

func parameterSQLType(kind codec.ParameterType) (string, error) {
	return querycore.ParameterSQLType(kind)
}
