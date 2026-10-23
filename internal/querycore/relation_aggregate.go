package querycore

import (
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RelationAggregateSelect builds one grouped batch for both relation APIs.
func RelationAggregateSelect(node SelectNode, inputTable, inputAlias string, group FieldRef, measure AggregateNode, singular bool, unique FieldRef, keys []driver.Value, limit int) (SelectNode, error) {
	field := func(f FieldRef) error { return f.Validate(inputTable) }
	if err := measure.Validate(field); err != nil {
		return SelectNode{}, err
	}
	nodes := 0
	if err := measure.ValidateFilter(field, &nodes); err != nil {
		return SelectNode{}, err
	}
	node.Selections = []SelectItem{{Expression: group}, {Expression: RequalifyAggregate(measure, inputAlias)}}
	node.GroupBy = []ValueExpression{group}
	node.Orders = nil
	node.Limit = value.Set(limit)
	if singular || unique != (FieldRef{}) {
		node.Selections = append(node.Selections, SelectItem{Expression: AggregateNode{Kind: CountAll}})
	}
	if unique != (FieldRef{}) {
		node.Selections = append(node.Selections, SelectItem{Expression: AggregateNode{Kind: CountDistinct, Field: unique}})
	}
	if keys != nil {
		bindings := make([]any, len(keys))
		for i, key := range keys {
			bindings[i] = key
		}
		node.Predicates = append(slices.Clone(node.Predicates), Comparison{Operand: group, Operator: In, Values: bindings, Bind: func(v any) (driver.Value, error) {
			if !driver.IsValue(v) {
				return nil, fault.New(fault.Invalid, "aggregate key is not a database value")
			}
			return v, nil
		}})
	}
	return node, nil
}
