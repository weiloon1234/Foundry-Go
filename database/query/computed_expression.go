package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func visitComputedValue(v valueExpression, item func(valueExpression) error, condition func(expression) error) (bool, error) {
	return querycore.VisitComputedValue(v, item, condition)
}

func validateRowValue(v valueExpression, field func(fieldRef) error, depth int, nodes *int) error {
	return querycore.ValidateRowValue(v, field, depth, nodes)
}

func mapComputedValue(v valueExpression, item func(valueExpression) valueExpression, condition func(expression) expression) (valueExpression, bool) {
	handled, err := visitComputedValue(v, func(valueExpression) error { return nil }, func(expression) error { return nil })
	if !handled {
		return v, false
	}
	if err != nil {
		return parameterNode{Err: err}, true
	}
	switch v := v.(type) {
	case operationNode:
		v.Arguments = slices.Clone(v.Arguments)
		for i, a := range v.Arguments {
			v.Arguments[i].Value = item(a.Value)
		}
		return v, true
	case conditionalNode:
		v.Arguments = slices.Clone(v.Arguments)
		for i, a := range v.Arguments {
			v.Arguments[i] = item(a)
		}
		return v, true
	case caseNode:
		v.Branches = slices.Clone(v.Branches)
		for i, b := range v.Branches {
			v.Branches[i] = caseBranch{Condition: condition(b.Condition), Result: item(b.Result)}
		}
		v.Otherwise = item(v.Otherwise)
		return v, true
	default:
		return v, true
	}
}
