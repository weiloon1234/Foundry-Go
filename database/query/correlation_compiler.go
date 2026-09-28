package query

import "github.com/weiloon1234/Foundry-Go/fault"

type outerScope struct {
	sources map[string]map[string]Column
	field   func(fieldRef) error
}

// Capture enclosing maps and grouping rules by value: recursive compilation
// changes compiler state. Correlation can access only its declared parent scope.
func (c *compiler) correlationScope(required *scopeRequirement, grouped map[fieldRef]bool, grouping bool) (*outerScope, error) {
	if required == nil {
		return nil, nil
	}
	if required.err != nil {
		return nil, required.err
	}
	if len(required.sources) == 0 || len(required.sources) > MaxExpressionNodes {
		return nil, fault.New(fault.Invalid, "correlated query has no valid outer scope")
	}
	local, enclosing, level := c.sources, c.outer, c.selectDepth
	visible := make(map[string]map[string]Column, len(required.sources))
	for name, expected := range required.sources {
		actual := local[name]
		if actual == nil && enclosing != nil {
			actual = enclosing.sources[name]
		}
		if actual == nil {
			return nil, fault.New(fault.Invalid, "correlated query belongs to a different outer source")
		}
		for _, column := range expected {
			if _, ok := actual[column.Name]; !ok {
				return nil, fault.New(fault.Invalid, "correlated outer column is not declared")
			}
		}
		visible[name] = actual
	}
	return &outerScope{sources: visible, field: func(f fieldRef) error {
		if _, ok := visible[f.table][f.column]; !ok {
			return fault.New(fault.Invalid, "query uses an undeclared outer column")
		}
		c.observeField(level, f)
		if local[f.table] != nil {
			if grouping && !grouped[f] {
				return fault.New(fault.Invalid, "correlated outer column is not grouped")
			}
			return f.validate(f.table)
		}
		if enclosing != nil {
			return enclosing.field(f)
		}
		return fault.New(fault.Invalid, "correlated query has no enclosing field scope")
	}}, nil
}
func (c *compiler) aggregateField(f fieldRef) error {
	if err := c.declaredField(f); err != nil {
		return err
	}
	if c.outer != nil && c.sources[f.table] == nil {
		return fault.New(fault.Invalid, "aggregate requires a field in its own SELECT; outer-only aggregates change SQL query ownership")
	}
	return nil
}
