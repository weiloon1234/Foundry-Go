package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type queryCore = querycore.QueryCore

type modelDefinition = querycore.ModelDefinition

type queryOrder = querycore.QueryOrder

func compileRead(node selectNode, scopeContext context.Context, transaction bool, err error, hasDecoder bool) (Statement, error) {
	result, err := querycore.CompileRead(node, scopeContext, transaction, err, hasDecoder)
	return statementFromCore(result), err
}

func appendQueryOrders[M any](base []queryOrder, orders []Order[M]) []queryOrder {
	result := make([]queryOrder, len(base)+len(orders))
	copy(result, base)
	for i, o := range orders {
		result[len(base)+i] = queryOrder{Field: o.field, Computed: o.computed, Descending: o.descending, Nulls: o.nulls}
	}
	return result
}
