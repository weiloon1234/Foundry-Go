package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Model fields and aggregate conditions share binding and comparison semantics.
func typedComparison[V any](operand valueExpression, op operator, c codec.Codec[V], values []V) comparison {
	bindings := make([]any, len(values))
	for i, v := range values {
		bindings[i] = c.Clone(v)
	}
	return comparison{operand: operand, operator: op, values: bindings, kind: c.ParameterType(), bind: func(raw any) (driver.Value, error) {
		v, ok := raw.(V)
		if !ok {
			return nil, fault.New(fault.Invalid, "invalid typed query binding")
		}
		return c.Bind(v)
	}}
}
