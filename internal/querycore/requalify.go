package querycore

// requalify is used only after the original model scope has been validated.
// It copies expression containers; the caller's predicates remain unchanged.
func Requalify(e Expression, alias string) Expression {
	switch e := e.(type) {
	case Comparison:
		e.Operand = RequalifyValue(e.Operand, alias)
		return e
	case SubqueryPredicate:
		e.Query = RequalifyCorrelation(e.Query, alias)
		if e.Operand != nil {
			e.Operand = RequalifyValue(e.Operand, alias)
		}
		return e
	case BinaryComparison:
		e.Left, e.Right = RequalifyValue(e.Left, alias), RequalifyValue(e.Right, alias)
		return e
	case RowComparison:
		operands := make([]ValueExpression, len(e.Operands))
		for i, operand := range e.Operands {
			operands[i] = RequalifyValue(operand, alias)
		}
		e.Operands = operands
		return e
	case ScopeNode:
		return e.Rewrite(func(resolved Expression) (Expression, error) { return Requalify(resolved, alias), nil })
	case Junction:
		children := make([]Expression, len(e.Children))
		for i, child := range e.Children {
			children[i] = Requalify(child, alias)
		}
		e.Children = children
		return e
	case Negation:
		return Negation{Child: Requalify(e.Child, alias)}
	default:
		return e
	}
}

func RequalifyValue(v ValueExpression, alias string) ValueExpression {
	if mapped, ok := MapComputedValue(v, func(v ValueExpression) ValueExpression { return RequalifyValue(v, alias) }, func(p Expression) Expression { return Requalify(p, alias) }); ok {
		return mapped
	}
	switch v := v.(type) {
	case FieldRef:
		v.Table = alias
		return v
	case AggregateNode:
		return RequalifyAggregate(v, alias)
	case ScalarSubquery:
		v.Query = RequalifyCorrelation(v.Query, alias)
		return v
	default:
		return v
	}
}

func RequalifyAggregate(a AggregateNode, alias string) AggregateNode {
	if a.Field != (FieldRef{}) {
		a.Field.Table = alias
	}
	if a.Filter != nil {
		predicates := make([]Expression, len(a.Filter.Predicates))
		for i, predicate := range a.Filter.Predicates {
			predicates[i] = Requalify(predicate, alias)
		}
		a.Filter = &AggregateFilter{Predicates: predicates}
	}
	return a
}
