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
		node.predicates = append(node.predicates, requalify(keys, throughPivotAlias))
	}
	node.limit = value.Set(limit)
	// Declaration validation compiles without a context: context scopes then
	// stand in as TRUE for shape checks only; execution always resolves them.
	c := compiler{scopeContext: ctx, scopeShapeOnly: ctx == nil}
	sql, err := c.compileSelect(node)
	if err != nil {
		return Statement{}, err
	}
	return Statement{sql: sql, arguments: c.arguments}, nil
}

func (r ThroughRelation[M, N, P]) throughSelect() selectNode {
	return r.throughSelectAt(throughTargetAlias, throughPivotAlias)
}
func (r ThroughRelation[M, N, P]) throughSelectAt(targetAlias, pivotAlias string) selectNode {
	target, pivot := r.spec.target, r.pivot
	node := selectNode{
		source:     tableSource{table: target.table, alias: targetAlias, columns: target.definition.columns},
		selections: append(columnSelections(targetAlias, target.definition.columns), columnSelections(pivotAlias, pivot.definition.columns)...),
		joins: []joinNode{{source: tableSource{table: pivot.table, alias: pivotAlias, columns: pivot.definition.columns},
			on: binaryComparison{fieldRef{targetAlias, r.spec.foreign.column}, fieldRef{pivotAlias, r.pivotForeign.column}, equal}}},
	}
	for _, p := range target.effectivePredicates() {
		node.predicates = append(node.predicates, requalify(p, targetAlias))
	}
	for _, p := range pivot.effectivePredicates() {
		node.predicates = append(node.predicates, requalify(p, pivotAlias))
	}
	seen := make(map[fieldRef]bool, len(r.orders))
	for _, o := range r.orders {
		alias := targetAlias
		if o.pivot {
			alias = pivotAlias
		}
		expr := requalifyValue(o.value, alias)
		node.orders = append(node.orders, orderNode{expr, o.descending, o.nulls})
		if field, plain := expr.(fieldRef); plain {
			seen[field] = true
		}
	}
	for _, f := range []fieldRef{{targetAlias, target.definition.primary}, {pivotAlias, pivot.definition.primary}} {
		if !seen[f] {
			node.orders = append(node.orders, orderNode{expression: f})
		}
	}
	return node
}
