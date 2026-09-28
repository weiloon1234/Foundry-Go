package validation

// MinItems and MaxItems validate ordinary or named slices. Nil and empty slices
// both have zero items; omitted/null transport states use their own wrappers.
func MinItems[S ~[]T, T any](minimum int) Rule[S] {
	if minimum < 0 {
		return failed[S](invalid("minimum item count must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.min_items", Parameters: []Parameter{parameter("min", minimum)}}, collectionWireTransform[S, T](), func(_ *execution, input S) (bool, error) { return len(input) >= minimum, nil })
}
func MaxItems[S ~[]T, T any](maximum int) Rule[S] {
	if maximum < 0 {
		return failed[S](invalid("maximum item count must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.max_items", Parameters: []Parameter{parameter("max", maximum)}}, collectionWireTransform[S, T](), func(_ *execution, input S) (bool, error) { return len(input) <= maximum, nil })
}

// Distinct checks scalar slice values with native equality and bounded work.
// It reports at the collection's path and does not normalize or mutate values.
func Distinct[S ~[]T, T Scalar]() Rule[S] {
	return valueRule(Spec{ID: "foundry.distinct"}, collectionWireTransform[S, T]() || scalarWireTransform[T](), func(s *execution, input S) (bool, error) {
		seen := make(map[T]struct{}, min(len(input), s.remainingChecks()))
		for _, item := range input {
			if !s.take(0) || !scalarValue(s, item) {
				return false, nil
			}
			if _, found := seen[item]; found {
				return false, nil
			}
			seen[item] = struct{}{}
		}
		return true, nil
	})
}
