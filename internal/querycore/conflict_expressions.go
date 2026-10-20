package querycore

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// One dependency node exposes index targets, assignment subqueries and update
// predicates to the existing bounded alias allocator and statement-wide CTE planner.
func (p Conflict) Dependencies(node SelectNode) SelectNode {
	node.Predicates = append(slices.Clone(p.Condition), p.RowCondition...)
	node.Predicates = append(node.Predicates, p.TargetCondition...)
	node.Selections = slices.Clone(node.Selections)
	for _, key := range p.Keys {
		node.Selections = append(node.Selections, SelectItem{Expression: key})
	}
	for _, u := range p.Updates {
		if u.Value != nil {
			node.Selections = append(node.Selections, SelectItem{Expression: u.Value})
		}
	}
	return node
}

func (p Conflict) ValidateRows(c *Compiler) error {
	sources := c.Sources
	c.Sources = map[string]map[string]Column{
		ConflictStoredTable:   c.Columns,
		ConflictProposedTable: c.Columns,
	}
	defer func() { c.Sources = sources }()
	nodes := 0
	for _, u := range p.Updates {
		if u.Value != nil {
			if err := ValidateRowValue(u.Value, c.DeclaredField, 0, &nodes); err != nil {
				return err
			}
		}
	}
	for _, condition := range p.RowCondition {
		if err := ValidateExpressionFields(condition, c.DeclaredField, 0, &nodes); err != nil {
			return err
		}
	}
	return nil
}

func (u ConflictUpdate) validMode() bool {
	count := 0
	if u.Incoming {
		count++
	}
	if u.Null {
		count++
	}
	if u.Literal != nil {
		if u.Literal.Bind == nil {
			return false
		}
		count++
	}
	if u.Value != nil {
		count++
	}
	return count == 1
}

func (u ConflictUpdate) compileValue(c *Compiler, rename *correlationRenamer) (string, error) {
	if u.Null {
		if !c.Columns[u.Field.Column].Nullable {
			return "", fault.New(fault.Invalid, "NULL conflict assignment requires a nullable model field")
		}
		return c.Parameter(nil)
	}
	if u.Incoming {
		return Qualified(FieldRef{Table: ConflictProposedTable, Column: u.Field.Column}), nil
	}
	if u.Value != nil {
		expression := rename.value(u.Value, 0)
		if rename.err != nil {
			return "", rename.err
		}
		return c.SelectedExpression(expression, nil, false)
	}
	if u.Literal == nil || u.Literal.Bind == nil {
		return "", fault.New(fault.Invalid, "conflict assignment has no value")
	}
	return c.AssignmentParameter(u.Field.Column, u.Literal.Bind)
}
