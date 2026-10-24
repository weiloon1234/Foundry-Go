package querycore

func HasPrimaryEquality(predicates []Expression, primary string) bool {
	for _, predicate := range predicates {
		switch predicate := predicate.(type) {
		case Comparison:
			if field, ok := UnwrapSensitivity(predicate.Operand).(FieldRef); ok && field.Column == primary && predicate.Operator == Equal {
				return true
			}
		case Junction:
			if !predicate.Any && HasPrimaryEquality(predicate.Children, primary) {
				return true
			}
		}
	}
	return false
}
