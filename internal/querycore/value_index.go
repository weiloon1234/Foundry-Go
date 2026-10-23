package querycore

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// A canonical value uses the shared compiler with local parameter numbering.
// Bound values participate in identity; SQL text alone is insufficient.
type PlannedValue struct {
	Sql       string
	Arguments []any
	Shape     string
}

func (c *Compiler) PlanValue(v ValueExpression) (PlannedValue, error) {
	probe := *c
	probe.Arguments = nil
	probe.Keys = nil
	text, err := probe.SelectedExpression(v, nil, false)
	c.SelectNodes, c.ExpressionNodes = probe.SelectNodes, probe.ExpressionNodes
	c.ScalarSQLBytes = probe.ScalarSQLBytes
	compiled := probe.Statement(text)
	return PlannedValue{Sql: compiled.SQLText, Arguments: probe.Arguments, Shape: compiled.Shape}, err
}

type ValueIndex struct {
	Values                 []PlannedValue
	Slots                  map[string][]int
	Comparisons, Arguments int
}

func (x *ValueIndex) Add(p PlannedValue) int {
	if x.Slots == nil {
		x.Slots = make(map[string][]int)
	}
	i := len(x.Values)
	x.Values = append(x.Values, p)
	x.Slots[p.Sql] = append(x.Slots[p.Sql], i)
	return i
}

func (x *ValueIndex) Find(p PlannedValue) (int, bool, error) {
	for _, i := range x.Slots[p.Sql] {
		x.Comparisons++
		x.Arguments += len(p.Arguments)
		if x.Comparisons > MaxExpressionNodes || x.Arguments > MaxParameters {
			return 0, false, fault.New(fault.Invalid, "expression matching exceeds its resource bound")
		}
		if slices.EqualFunc(x.Values[i].Arguments, p.Arguments, reflect.DeepEqual) {
			return i, true, nil
		}
	}
	return 0, false, nil
}
