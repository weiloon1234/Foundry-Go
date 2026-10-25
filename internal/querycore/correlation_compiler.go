package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type OuterScope struct {
	Sources map[string]map[string]Column
	Field   func(FieldRef) error
}

// Capture enclosing maps and grouping rules by value: recursive compilation
// changes compiler state. Correlation can access only its declared parent scope.
func (c *Compiler) CorrelationScope(required *ScopeRequirement, grouped map[FieldRef]bool, grouping bool) (*OuterScope, error) {
	if required == nil {
		return nil, nil
	}
	if required.Err != nil {
		return nil, required.Err
	}
	if len(required.Sources) == 0 || len(required.Sources) > MaxExpressionNodes {
		return nil, safeerror.NewFault(fault.Invalid, "correlated query has no valid outer scope")
	}
	local, enclosing, level := c.Sources, c.Outer, c.SelectDepth
	visible := make(map[string]map[string]Column, len(required.Sources))
	for name, expected := range required.Sources {
		actual := local[name]
		if actual == nil && enclosing != nil {
			actual = enclosing.Sources[name]
		}
		if actual == nil {
			return nil, safeerror.NewFault(fault.Invalid, "correlated query belongs to a different outer source")
		}
		for _, column := range expected {
			if _, ok := actual[column.Name]; !ok {
				return nil, safeerror.NewFault(fault.Invalid, "correlated outer column is not declared")
			}
		}
		visible[name] = actual
	}
	return &OuterScope{Sources: visible, Field: func(f FieldRef) error {
		if _, ok := visible[f.Table][f.Column]; !ok {
			return safeerror.NewFault(fault.Invalid, "query uses an undeclared outer column")
		}
		c.ObserveField(level, f)
		if local[f.Table] != nil {
			if grouping && !grouped[f] {
				return safeerror.NewFault(fault.Invalid, "correlated outer column is not grouped")
			}
			return f.Validate(f.Table)
		}
		if enclosing != nil {
			return enclosing.Field(f)
		}
		return safeerror.NewFault(fault.Invalid, "correlated query has no enclosing field scope")
	}}, nil
}

func (c *Compiler) AggregateField(f FieldRef) error {
	if err := c.DeclaredField(f); err != nil {
		return err
	}
	if c.Outer != nil && c.Sources[f.Table] == nil {
		return safeerror.NewFault(fault.Invalid, "aggregate requires a field in its own SELECT; outer-only aggregates change SQL query ownership")
	}
	return nil
}
