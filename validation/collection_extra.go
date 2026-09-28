package validation

// Items requires exactly count elements, including on named slice types.
func Items[S ~[]T, T any](count int) Rule[S] { return ItemsBetween[S](count, count) }
func ItemsBetween[S ~[]T, T any](minimum, maximum int) Rule[S] {
	if minimum > maximum {
		return failed[S](invalid("item count bounds are reversed"))
	}
	return Bail(MinItems[S](minimum), MaxItems[S](maximum))
}

// DistinctBy checks a concrete field of each object without serializing objects.
// Failures belong to the collection; use Each for per-object value constraints.
func DistinctBy[S ~[]T, T any, K Scalar](field Field[T, K]) Rule[S] {
	if err := field.Validate(); err != nil {
		return failed[S](err)
	}
	return valueRule(Spec{ID: "foundry.distinct_by", Parameters: []Parameter{parameter("field", field.name)}}, true, func(s *execution, input S) (bool, error) {
		seen := make(map[K]struct{}, min(len(input), s.remainingChecks()))
		for _, item := range input {
			if !s.take(0) {
				return false, nil
			}
			key := field.selectValue(item)
			if !scalarValue(s, key) {
				return false, nil
			}
			if _, exists := seen[key]; exists {
				return false, nil
			}
			seen[key] = struct{}{}
		}
		return true, nil
	})
}

// ContainsItems requires every declared scalar member at least once. It owns
// the declaration and scans input once; ExcludesItems prohibits any member.
func ContainsItems[S ~[]T, T Scalar](members ...T) Rule[S] { return itemsMembership[S](true, members) }
func ExcludesItems[S ~[]T, T Scalar](members ...T) Rule[S] { return itemsMembership[S](false, members) }
func itemsMembership[S ~[]T, T Scalar](contains bool, members []T) Rule[S] {
	allowed := OneOf(members...)
	if err := allowed.Validate(); err != nil {
		return failed[S](err)
	}
	owned := make(map[T]struct{}, len(members))
	for _, member := range members {
		owned[member] = struct{}{}
	}
	spec := Spec{ID: "foundry.contains_items", Parameters: allowed.info.Spec.Parameters}
	if !contains {
		spec.ID = "foundry.excludes_items"
	}
	return valueRule(spec, true, func(s *execution, input S) (bool, error) {
		found := make(map[T]struct{}, min(len(owned), len(input)))
		for _, item := range input {
			if !s.take(0) || !scalarValue(s, item) {
				return false, nil
			}
			if _, exists := owned[item]; exists {
				if !contains {
					return false, nil
				}
				found[item] = struct{}{}
			}
		}
		return !contains || len(found) == len(owned), nil
	})
}
