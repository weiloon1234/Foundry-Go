package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type projectionColumn = querycore.ProjectionColumn

type projectionMapping = querycore.ProjectionMapping

func validateProjectionColumns(columns []projectionColumn, hasDecoder bool) error {
	return querycore.ValidateProjectionColumns(columns, hasDecoder)
}

func selectProjection(source selectNode, columns []projectionColumn, mappings []projectionMapping) (selectNode, error) {
	return querycore.SelectProjection(source, columns, mappings)
}
