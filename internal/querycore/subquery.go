package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

type Subquery struct {
	Node        SelectNode
	Err         error
	Correlation *ScopeRequirement
}

type SubqueryPredicate struct {
	Query   Subquery
	Operand ValueExpression // nil means EXISTS; otherwise single-column IN.
}

func (SubqueryPredicate) ExpressionNode() {}

type ScalarSubquery struct{ Query Subquery }

func (ScalarSubquery) ValueNode() {}

func (c *Compiler) SubquerySQL(q Subquery, single bool, grouped map[FieldRef]bool, grouping bool) (string, error) {
	if q.Err != nil {
		return "", q.Err
	}
	if single && len(q.Node.Selections) != 1 {
		return "", fault.New(fault.Invalid, "value subquery must select exactly one column")
	}
	outer, err := c.CorrelationScope(q.Correlation, grouped, grouping)
	if err != nil {
		return "", err
	}
	return c.SelectSQLWithOuter(q.Node, outer)
}

func (c *Compiler) SubqueryPredicate(p SubqueryPredicate, grouped map[FieldRef]bool, grouping bool) (string, error) {
	var operand string
	if p.Operand != nil {
		var err error
		operand, err = c.SelectedExpression(p.Operand, grouped, grouping)
		if err != nil {
			return "", err
		}
	}
	text, err := c.SubquerySQL(p.Query, p.Operand != nil, grouped, grouping)
	if err != nil {
		return "", err
	}
	if p.Operand == nil {
		return "EXISTS(" + text + ")", nil
	}
	return "(" + operand + " IN (" + text + "))", nil
}
