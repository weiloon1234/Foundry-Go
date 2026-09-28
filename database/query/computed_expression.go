package query

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// One structural boundary is shared by compilation, analysis, validation and
// qualification. Child callbacks retain the caller's row/group/window context.
func visitComputedValue(v valueExpression, item func(valueExpression) error, condition func(expression) error) (bool, error) {
	switch v := v.(type) {
	case operationNode:
		if err := v.validate(); err != nil {
			return true, err
		}
		for _, argument := range v.arguments {
			if err := item(argument.value); err != nil {
				return true, err
			}
		}
		return true, nil
	case parameterNode:
		return true, v.validate()
	case conditionalNode:
		if v.err != nil {
			return true, v.err
		}
		if len(v.arguments) == 0 || len(v.arguments) > MaxExpressionNodes || (v.kind != coalesceValues && v.kind != nullIfValues) || (v.kind == nullIfValues && len(v.arguments) != 2) {
			return true, fault.New(fault.Invalid, "invalid conditional value arguments")
		}
		for _, argument := range v.arguments {
			if err := item(argument); err != nil {
				return true, err
			}
		}
		return true, nil
	case caseNode:
		if v.err != nil {
			return true, v.err
		}
		if len(v.branches) == 0 || len(v.branches) > MaxExpressionNodes || v.otherwise == nil {
			return true, fault.New(fault.Invalid, "CASE requires bounded branches and an explicit ELSE result")
		}
		for _, branch := range v.branches {
			if err := condition(branch.condition); err != nil {
				return true, err
			}
			if err := item(branch.result); err != nil {
				return true, err
			}
		}
		return true, item(v.otherwise)
	default:
		return false, nil
	}
}

func validateRowValue(v valueExpression, field func(fieldRef) error, depth int, nodes *int) error {
	if f, ok := v.(fieldRef); ok && depth == 0 {
		return field(f)
	}
	*nodes++
	if depth > MaxExpressionDepth || *nodes > MaxExpressionNodes {
		return fault.New(fault.Invalid, "row value exceeds its resource bound")
	}
	if f, ok := v.(fieldRef); ok {
		return field(f)
	}
	if s, ok := v.(scalarSubquery); ok {
		// The subquery owns an independent SELECT phase. Its scopes, cardinality
		// and nested expression budget are checked by the shared query compiler.
		return s.query.err
	}
	check := func(v valueExpression) error { return validateRowValue(v, field, depth+1, nodes) }
	handled, err := visitComputedValue(v, check, func(p expression) error { return validateExpressionValues(p, check, depth+1, nodes) })
	if handled {
		return err
	}
	return fault.New(fault.Invalid, "row values cannot contain selected aggregate or window expressions")
}

func mapComputedValue(v valueExpression, item func(valueExpression) valueExpression, condition func(expression) expression) (valueExpression, bool) {
	handled, err := visitComputedValue(v, func(valueExpression) error { return nil }, func(expression) error { return nil })
	if !handled {
		return v, false
	}
	if err != nil {
		return parameterNode{err: err}, true
	}
	switch v := v.(type) {
	case operationNode:
		v.arguments = slices.Clone(v.arguments)
		for i, a := range v.arguments {
			v.arguments[i].value = item(a.value)
		}
		return v, true
	case conditionalNode:
		v.arguments = slices.Clone(v.arguments)
		for i, a := range v.arguments {
			v.arguments[i] = item(a)
		}
		return v, true
	case caseNode:
		v.branches = slices.Clone(v.branches)
		for i, b := range v.branches {
			v.branches[i] = caseBranch{condition(b.condition), item(b.result)}
		}
		v.otherwise = item(v.otherwise)
		return v, true
	default:
		return v, true
	}
}

func (c *compiler) computedSQL(v valueExpression, grouped map[fieldRef]bool, grouping bool) (string, error) {
	switch v := v.(type) {
	case operationNode:
		return c.operationSQL(v, grouped, grouping)
	case parameterNode:
		return c.parameterSQL(v)
	case conditionalNode:
		arguments := make([]string, len(v.arguments))
		for i, argument := range v.arguments {
			text, err := c.selectedExpression(argument, grouped, grouping)
			if err != nil {
				return "", err
			}
			arguments[i] = text
		}
		name := "COALESCE"
		if v.kind == nullIfValues {
			name = "NULLIF"
		}
		return name + "(" + strings.Join(arguments, ", ") + ")", nil
	case caseNode:
		var result strings.Builder
		result.WriteString("CASE")
		for _, branch := range v.branches {
			condition, err := c.expressionAt(branch.condition, grouped, grouping)
			if err != nil {
				return "", err
			}
			item, err := c.selectedExpression(branch.result, grouped, grouping)
			if err != nil {
				return "", err
			}
			result.WriteString(" WHEN " + condition + " THEN " + item)
		}
		otherwise, err := c.selectedExpression(v.otherwise, grouped, grouping)
		if err != nil {
			return "", err
		}
		result.WriteString(" ELSE " + otherwise + " END")
		return result.String(), nil
	default:
		return "", fault.New(fault.Invalid, "invalid computed SQL value")
	}
}
