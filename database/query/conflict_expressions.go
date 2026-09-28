package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// One dependency node exposes index targets, assignment subqueries and update
// predicates to the existing bounded alias allocator and statement-wide CTE planner.
func (p Conflict[M]) dependencies(node selectNode) selectNode {
	node.predicates = append(slices.Clone(p.condition), p.rowCondition...)
	node.predicates = append(node.predicates, p.targetCondition...)
	node.selections = slices.Clone(node.selections)
	for _, key := range p.keys {
		node.selections = append(node.selections, selectItem{expression: key})
	}
	for _, u := range p.updates {
		if u.value != nil {
			node.selections = append(node.selections, selectItem{expression: u.value})
		}
	}
	return node
}

func (p Conflict[M]) validateRows(c *compiler) error {
	sources := c.sources
	c.sources = map[string]map[string]Column{
		conflictStoredTable:   c.columns,
		conflictProposedTable: c.columns,
	}
	defer func() { c.sources = sources }()
	nodes := 0
	for _, u := range p.updates {
		if u.value != nil {
			if err := validateRowValue(u.value, c.declaredField, 0, &nodes); err != nil {
				return err
			}
		}
	}
	for _, condition := range p.rowCondition {
		if err := validateExpressionFields(condition, c.declaredField, 0, &nodes); err != nil {
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
		if u.literal.bind == nil {
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
		if !c.columns[u.field.column].Nullable {
			return "", fault.New(fault.Invalid, "NULL conflict assignment requires a nullable model field")
		}
		return c.parameter(nil)
	}
	if u.incoming {
		return qualified(fieldRef{conflictProposedTable, u.field.column}), nil
	}
	if u.value != nil {
		expression := rename.value(u.value, 0)
		if rename.err != nil {
			return "", rename.err
		}
		return c.selectedExpression(expression, nil, false)
	}
	if u.literal == nil || u.literal.bind == nil {
		return "", fault.New(fault.Invalid, "conflict assignment has no value")
	}
	return c.assignmentParameter(u.field.column, u.literal.bind)
}
