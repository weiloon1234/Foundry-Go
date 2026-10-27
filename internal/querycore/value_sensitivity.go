package querycore

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

import "github.com/weiloon1234/Foundry-Go/fault"

// SensitiveValue retains a codec's mark when a computation chooses a different
// result codec. It changes neither SQL, scope, grouping nor the value's type.
type SensitiveValue struct{ Value ValueExpression }

func (SensitiveValue) ValueNode() {}

func WithSensitivity(v ValueExpression, sensitive bool) ValueExpression {
	if !sensitive {
		return v
	}
	if _, ok := v.(SensitiveValue); ok {
		return v
	}
	return SensitiveValue{Value: v}
}

// UnwrapSensitivity is only for SQL shape/identity checks. Stored ASTs retain
// their annotations; the ordinary expression visitor validates their children.
func UnwrapSensitivity(v ValueExpression) ValueExpression {
	for depth := 0; depth <= MaxExpressionDepth; depth++ {
		mark, ok := v.(SensitiveValue)
		if !ok {
			return v
		}
		v = mark.Value
	}
	return ParameterNode{Err: safeerror.NewFault(fault.Invalid, "value sensitivity exceeds its depth bound")}
}
