package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/value"
)

const throughTargetAlias = "foundry_target"

const throughPivotAlias = "foundry_pivot"

func (r ThroughRelation[M, N, P]) compileThrough(ctx context.Context, keys expression, limit int) (Statement, error) {
	node := r.throughSelect()
	if keys != nil {
		node.Predicates = append(node.Predicates, requalify(keys, throughPivotAlias))
	}
	node.Limit = value.Set(limit)
	// Declaration validation compiles without a context: context scopes then
	// stand in as TRUE for shape checks only; execution always resolves them.
	c := compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
	sql, err := c.CompileSelect(node)
	if err != nil {
		return Statement{}, err
	}
	return Statement{sql: sql, arguments: c.Arguments}, nil
}

func (r ThroughRelation[M, N, P]) throughSelect() selectNode {
	return r.throughSelectAt(throughTargetAlias, throughPivotAlias)
}

func (r ThroughRelation[M, N, P]) throughSelectAt(targetAlias, pivotAlias string) selectNode {
	target, pivot := r.spec.target, r.pivot
	node := selectNode{
		Source:     tableSource{Table: target.queryCore.Table, Alias: targetAlias, Columns: target.definition.modelDefinition.Columns},
		Selections: append(columnSelections(targetAlias, target.definition.modelDefinition.Columns), columnSelections(pivotAlias, pivot.definition.modelDefinition.Columns)...),
		Joins: []joinNode{{Source: tableSource{Table: pivot.queryCore.Table, Alias: pivotAlias, Columns: pivot.definition.modelDefinition.Columns},
			On: binaryComparison{Left: fieldRef{Table: targetAlias, Column: r.spec.foreign.Column}, Right: fieldRef{Table: pivotAlias, Column: r.pivotForeign.Column}, Operator: equal}}},
	}
	for _, p := range target.queryCore.EffectivePredicates() {
		node.Predicates = append(node.Predicates, requalify(p, targetAlias))
	}
	for _, p := range pivot.queryCore.EffectivePredicates() {
		node.Predicates = append(node.Predicates, requalify(p, pivotAlias))
	}
	seen := make(map[fieldRef]bool, len(r.orders))
	for _, o := range r.orders {
		alias := targetAlias
		if o.pivot {
			alias = pivotAlias
		}
		expr := requalifyValue(o.value, alias)
		node.Orders = append(node.Orders, orderNode{Expression: expr, Descending: o.descending, Nulls: o.nulls})
		if field, plain := expr.(fieldRef); plain {
			seen[field] = true
		}
	}
	for _, f := range []fieldRef{{Table: targetAlias, Column: target.definition.modelDefinition.Primary}, {Table: pivotAlias, Column: pivot.definition.modelDefinition.Primary}} {
		if !seen[f] {
			node.Orders = append(node.Orders, orderNode{Expression: f})
		}
	}
	return node
}
