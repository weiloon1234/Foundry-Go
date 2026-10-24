package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

type ValueExpression interface{ ValueNode() }

func (FieldRef) ValueNode() {}

func (AggregateNode) ValueNode() {}

type SelectItem struct {
	Sensitive  bool // Explicit result-codec sensitivity, independent of its SQL expression.
	Expression ValueExpression
	Alias      string
}

func ColumnSelections(table string, columns []Column) []SelectItem {
	result := make([]SelectItem, len(columns))
	for i, c := range columns {
		result[i] = SelectItem{Expression: FieldRef{table, c.Name}}
	}
	return result
}

func (c *Compiler) SelectedExpression(e ValueExpression, grouped map[FieldRef]bool, grouping bool) (string, error) {
	if err := c.ValidateSelectedExpression(e, grouped, grouping); err != nil {
		return "", err
	}
	key, err := c.MatchingKey(e, false)
	if err != nil {
		return "", err
	}
	if key != nil {
		return c.KeySQL(key, grouped, grouping)
	}
	return c.RenderSelectedExpression(e, grouped, grouping)
}

func (c *Compiler) RenderSelectedExpression(e ValueExpression, grouped map[FieldRef]bool, grouping bool) (string, error) {
	switch e := e.(type) {
	case RawFragment:
		return c.RawSQL(e)
	case SensitiveValue:
		return c.RenderSelectedExpression(e.Value, grouped, grouping)
	case FieldRef:
		if c.IndexTarget {
			return Quoted(e.Column), nil
		}
		return Qualified(e), nil
	case AggregateNode:
		return c.Aggregate(e)
	case ScalarSubquery:
		text, err := c.SubquerySQL(e.Query, true, grouped, grouping)
		return "(" + text + ")", err
	case WindowNode:
		return c.WindowSQL(e, grouped, grouping)
	case ParameterNode, ConditionalNode, CaseNode, OperationNode:
		return c.ComputedSQL(e, grouped, grouping)
	default:
		return "", fault.New(fault.Invalid, "invalid selected expression")
	}
}

// Validation is pure: nested SELECTs bind only at their SQL output position.
func (c *Compiler) ValidateSelectedExpression(e ValueExpression, grouped map[FieldRef]bool, grouping bool) error {
	c.ValueDepth++
	defer func() { c.ValueDepth-- }()
	if c.ValueDepth > MaxExpressionDepth {
		return fault.New(fault.Invalid, "selected value exceeds its depth bound")
	}
	if _, field := e.(FieldRef); !field || c.ValueDepth > 1 {
		c.ExpressionNodes++
		if c.ExpressionNodes > MaxExpressionNodes {
			return fault.New(fault.Invalid, "selected value exceeds its resource bound")
		}
	}
	if _, field := e.(FieldRef); grouping && !field {
		key, err := c.MatchingKey(e, true)
		if err != nil {
			return err
		}
		if key != nil {
			return nil
		}
	}
	handled, err := VisitComputedValue(e, func(v ValueExpression) error { return c.ValidateSelectedExpression(v, grouped, grouping) }, func(p Expression) error {
		return ValidateExpressionValues(p, func(v ValueExpression) error { return c.ValidateSelectedExpression(v, grouped, grouping) }, 0, &c.ExpressionNodes)
	})
	if handled {
		return err
	}
	switch e := e.(type) {
	case FieldRef:
		if err := c.DeclaredField(e); err != nil {
			return err
		}
		outer := c.Outer != nil && c.Outer.Sources[e.Table] != nil
		if grouping && !outer && !grouped[e] {
			return fault.New(fault.Invalid, "selected column is not grouped")
		}
		return nil
	case AggregateNode:
		if c.DirectSelf {
			return fault.New(fault.Invalid, "aggregate functions are not allowed at a recursive self-reference SELECT level")
		}
		if err := e.Validate(c.AggregateField); err != nil {
			return err
		}
		return e.ValidateFilter(c.DeclaredField, &c.ExpressionNodes)
	case ScalarSubquery:
		return e.Query.Err
	case WindowNode:
		return c.ValidateWindow(e, grouped, grouping)
	default:
		return fault.New(fault.Invalid, "invalid selected expression")
	}
}

func (c *Compiler) ValidateHavingValue(v ValueExpression, grouped map[FieldRef]bool) error {
	previous := c.InWindow
	c.InWindow = true
	defer func() { c.InWindow = previous }()
	return c.ValidateSelectedExpression(v, grouped, true)
}
