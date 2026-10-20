package query

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

func validateRowValue(v valueExpression, field func(fieldRef) error, depth int, nodes *int) error {
	return querycore.ValidateRowValue(v, field, depth, nodes)
}
