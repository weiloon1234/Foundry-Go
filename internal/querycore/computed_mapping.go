package querycore

import "slices"

func MapComputedValue(v ValueExpression, item func(ValueExpression) ValueExpression, condition func(Expression) Expression) (ValueExpression, bool) {
	handled, err := VisitComputedValue(v, func(ValueExpression) error { return nil }, func(Expression) error { return nil })
	if !handled {
		return v, false
	}
	if err != nil {
		return ParameterNode{Err: err}, true
	}
	switch v := v.(type) {
	case SensitiveValue:
		return SensitiveValue{Value: item(v.Value)}, true
	case OperationNode:
		v.Arguments = slices.Clone(v.Arguments)
		for i, a := range v.Arguments {
			v.Arguments[i].Value = item(a.Value)
		}
		return v, true
	case ConditionalNode:
		v.Arguments = slices.Clone(v.Arguments)
		for i, a := range v.Arguments {
			v.Arguments[i] = item(a)
		}
		return v, true
	case CaseNode:
		v.Branches = slices.Clone(v.Branches)
		for i, b := range v.Branches {
			v.Branches[i] = CaseBranch{Condition: condition(b.Condition), Result: item(b.Result)}
		}
		v.Otherwise = item(v.Otherwise)
		return v, true
	default:
		return v, true
	}
}
