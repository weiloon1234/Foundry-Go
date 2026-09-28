package query

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// A canonical value uses the shared compiler with local parameter numbering.
// Bound values participate in identity; SQL text alone is insufficient.
type plannedValue struct {
	sql       string
	arguments []any
}

func (c *compiler) planValue(v valueExpression) (plannedValue, error) {
	probe := *c
	probe.arguments = nil
	probe.keys = nil
	text, err := probe.selectedExpression(v, nil, false)
	c.selectNodes, c.expressionNodes = probe.selectNodes, probe.expressionNodes
	c.scalarSQLBytes = probe.scalarSQLBytes
	return plannedValue{sql: text, arguments: probe.arguments}, err
}

type valueIndex struct {
	values                 []plannedValue
	slots                  map[string][]int
	comparisons, arguments int
}

func (x *valueIndex) add(p plannedValue) int {
	if x.slots == nil {
		x.slots = make(map[string][]int)
	}
	i := len(x.values)
	x.values = append(x.values, p)
	x.slots[p.sql] = append(x.slots[p.sql], i)
	return i
}

func (x *valueIndex) find(p plannedValue) (int, bool, error) {
	for _, i := range x.slots[p.sql] {
		x.comparisons++
		x.arguments += len(p.arguments)
		if x.comparisons > MaxExpressionNodes || x.arguments > MaxParameters {
			return 0, false, fault.New(fault.Invalid, "expression matching exceeds its resource bound")
		}
		if slices.EqualFunc(x.values[i].arguments, p.arguments, reflect.DeepEqual) {
			return i, true, nil
		}
	}
	return 0, false, nil
}
