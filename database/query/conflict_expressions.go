package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// One dependency node exposes index targets, assignment subqueries and update
// predicates to the existing bounded alias allocator and statement-wide CTE planner.
func (p Conflict[M]) dependencies(node selectNode) selectNode {
	node.Predicates = append(slices.Clone(p.condition), p.rowCondition...)
	node.Predicates = append(node.Predicates, p.targetCondition...)
	node.Selections = slices.Clone(node.Selections)
	for _, key := range p.keys {
		node.Selections = append(node.Selections, selectItem{Expression: key})
	}
	for _, u := range p.updates {
		if u.value != nil {
			node.Selections = append(node.Selections, selectItem{Expression: u.value})
		}
	}
	return node
}

func (p Conflict[M]) validateRows(c *compiler) error {
	sources := c.Sources
	c.Sources = map[string]map[string]columnData{
		conflictStoredTable:   c.Columns,
		conflictProposedTable: c.Columns,
	}
	defer func() { c.Sources = sources }()
	nodes := 0
	for _, u := range p.updates {
		if u.value != nil {
			if err := validateRowValue(u.value, c.DeclaredField, 0, &nodes); err != nil {
				return err
			}
		}
	}
	for _, condition := range p.rowCondition {
		if err := validateExpressionFields(condition, c.DeclaredField, 0, &nodes); err != nil {
			return err
		}
	}
	return nil
}

func (u ConflictUpdate[M]) validMode() bool {
	count := 0
	if u.incoming {
		count++
	}
	if u.null {
		count++
	}
	if u.literal != nil {
		if u.literal.Bind == nil {
			return false
		}
		count++
	}
	if u.value != nil {
		count++
	}
	return count == 1
}

func (u ConflictUpdate[M]) compileValue(c *compiler, rename *correlationRenamer) (string, error) {
	if u.null {
		if !c.Columns[u.field.Column].Nullable {
			return "", fault.New(fault.Invalid, "NULL conflict assignment requires a nullable model field")
		}
		return c.Parameter(nil)
	}
	if u.incoming {
		return qualified(fieldRef{Table: conflictProposedTable, Column: u.field.Column}), nil
	}
	if u.value != nil {
		expression := rename.value(u.value, 0)
		if rename.err != nil {
			return "", rename.err
		}
		return c.SelectedExpression(expression, nil, false)
	}
	if u.literal == nil || u.literal.Bind == nil {
		return "", fault.New(fault.Invalid, "conflict assignment has no value")
	}
	return c.AssignmentParameter(u.field.Column, u.literal.Bind)
}
