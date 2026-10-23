package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// One structural boundary is shared by compilation, analysis, validation and
// qualification. Child callbacks retain the caller's row/group/window context.
func VisitComputedValue(v ValueExpression, item func(ValueExpression) error, condition func(Expression) error) (bool, error) {
	switch v := v.(type) {
	case OperationNode:
		if err := v.Validate(); err != nil {
			return true, err
		}
		for _, argument := range v.Arguments {
			if err := item(argument.Value); err != nil {
				return true, err
			}
		}
		return true, nil
	case ParameterNode:
		return true, v.Validate()
	case RawFragment:
		return true, v.Validate()
	case ConditionalNode:
		if v.Err != nil {
			return true, v.Err
		}
		if err := ValidateConditionalArguments(v.Kind, len(v.Arguments)); err != nil {
			return true, err
		}
		for _, argument := range v.Arguments {
			if err := item(argument); err != nil {
				return true, err
			}
		}
		return true, nil
	case CaseNode:
		if v.Err != nil {
			return true, v.Err
		}
		if len(v.Branches) == 0 || len(v.Branches) > MaxExpressionNodes || v.Otherwise == nil {
			return true, fault.New(fault.Invalid, "CASE requires bounded branches and an explicit ELSE result")
		}
		for _, branch := range v.Branches {
			if err := condition(branch.Condition); err != nil {
				return true, err
			}
			if err := item(branch.Result); err != nil {
				return true, err
			}
		}
		return true, item(v.Otherwise)
	default:
		return false, nil
	}
}

func ValidateRowValue(v ValueExpression, field func(FieldRef) error, depth int, nodes *int) error {
	if f, ok := v.(FieldRef); ok && depth == 0 {
		return field(f)
	}
	*nodes++
	if depth > MaxExpressionDepth || *nodes > MaxExpressionNodes {
		return fault.New(fault.Invalid, "row value exceeds its resource bound")
	}
	if f, ok := v.(FieldRef); ok {
		return field(f)
	}
	if _, raw := v.(RawFragment); raw {
		return rawInvalid("selected fragment cannot be used as a row value")
	}
	if s, ok := v.(ScalarSubquery); ok {
		// The subquery owns an independent SELECT phase. Its scopes, cardinality
		// and nested expression budget are checked by the shared query compiler.
		return s.Query.Err
	}
	check := func(v ValueExpression) error { return ValidateRowValue(v, field, depth+1, nodes) }
	handled, err := VisitComputedValue(v, check, func(p Expression) error { return ValidateExpressionValues(p, check, depth+1, nodes) })
	if handled {
		return err
	}
	return fault.New(fault.Invalid, "row values cannot contain selected aggregate or window expressions")
}

func (c *Compiler) ComputedSQL(v ValueExpression, grouped map[FieldRef]bool, grouping bool) (string, error) {
	switch v := v.(type) {
	case OperationNode:
		return c.OperationSQL(v, grouped, grouping)
	case ParameterNode:
		return c.ParameterSQL(v)
	case ConditionalNode:
		arguments := make([]string, len(v.Arguments))
		for i, argument := range v.Arguments {
			text, err := c.SelectedExpression(argument, grouped, grouping)
			if err != nil {
				return "", err
			}
			arguments[i] = text
		}
		name := "COALESCE"
		if v.Kind == NullIfValues {
			name = "NULLIF"
		}
		return name + "(" + strings.Join(arguments, ", ") + ")", nil
	case CaseNode:
		var result strings.Builder
		result.WriteString("CASE")
		for _, branch := range v.Branches {
			condition, err := c.ExpressionAt(branch.Condition, grouped, grouping)
			if err != nil {
				return "", err
			}
			item, err := c.SelectedExpression(branch.Result, grouped, grouping)
			if err != nil {
				return "", err
			}
			result.WriteString(" WHEN " + condition + " THEN " + item)
		}
		otherwise, err := c.SelectedExpression(v.Otherwise, grouped, grouping)
		if err != nil {
			return "", err
		}
		result.WriteString(" ELSE " + otherwise + " END")
		return result.String(), nil
	default:
		return "", fault.New(fault.Invalid, "invalid computed SQL value")
	}
}
