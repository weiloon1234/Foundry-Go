package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"

	"github.com/weiloon1234/Foundry-Go/value"
)

const throughTargetAlias = querycore.RelationTargetAlias
const throughPivotAlias = querycore.RelationPivotAlias

func (r ThroughRelation[M, N, P]) compileThrough(ctx context.Context, keys expression, limit int) (Statement, error) {
	node := r.throughSelect()
	if keys != nil {
		node.Predicates = append(node.Predicates, requalify(keys, throughPivotAlias))
	}
	node.Limit = value.Set(limit)
	// Declaration validation compiles without a context: context scopes then
	// stand in as TRUE for shape checks only; execution always resolves them.
	c := compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
	c.BeginStatement(ctx)
	sql, err := c.CompileSelect(node)
	if err != nil {
		return Statement{}, err
	}
	return statementFromCore(c.Statement(sql)), nil
}

func (r ThroughRelation[M, N, P]) throughSelect() selectNode {
	return r.throughSelectAt(throughTargetAlias, throughPivotAlias)
}

func (r ThroughRelation[M, N, P]) throughSelectAt(targetAlias, pivotAlias string) selectNode {
	orders := make([]querycore.RelationOrder, len(r.orders))
	for i, o := range r.orders {
		orders[i] = querycore.RelationOrder{Value: o.value, Descending: o.descending, Pivot: o.pivot, Nulls: o.nulls}
	}
	return querycore.RelationThroughSelect(r.spec.target.queryCore, r.pivot.queryCore, r.spec.foreign, r.pivotForeign, orders, targetAlias, pivotAlias)
}
