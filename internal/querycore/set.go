package querycore

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// SetAlias is the output scope shared by record and scalar set adapters.
const SetAlias = "foundry_set"

type SetOperator uint8

const (
	UnionSet SetOperator = iota
	UnionAllSet
	IntersectSet
	IntersectAllSet
	ExceptSet
	ExceptAllSet
)

type SetNode struct {
	Left, Right SelectNode
	Operator    SetOperator
}

func (c *Compiler) SetSQL(s SetNode, columns []Column) (string, error) {
	if s.Operator > ExceptAllSet || len(columns) == 0 || len(columns) != len(s.Left.Selections) || len(columns) != len(s.Right.Selections) {
		return "", safeerror.NewFault(fault.Invalid, "invalid set operator or output layout")
	}
	// Explicit output aliases make model columns and declared projection columns
	// interchangeable only after the typed record boundary has matched layouts.
	left := s.Left
	left.Selections = slices.Clone(left.Selections)
	for i, column := range columns {
		left.Selections[i].Alias = column.Name
	}
	a, err := c.SelectSQL(left)
	if err != nil {
		return "", err
	}
	b, err := c.SelectSQL(s.Right)
	if err != nil {
		return "", err
	}
	operator := [...]string{"UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL"}[s.Operator]
	return "(" + a + ") " + operator + " (" + b + ")", nil
}
