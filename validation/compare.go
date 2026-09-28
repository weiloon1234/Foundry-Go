package validation

import "github.com/weiloon1234/Foundry-Go/internal/jsonpointer"

// Pair retains both values' concrete type for reusable comparison rules.
type Pair[V any] struct{ Left, Right V }

// Scalar includes ordinary and named numeric, text and boolean values.
type Scalar interface {
	Number | ~string | ~bool
}

// Same requires equal scalar values without coercion or normalization.
func Same[V Scalar]() Rule[Pair[V]] {
	return valueRule(Spec{ID: "foundry.same"}, scalarWireTransform[V](), func(s *execution, pair Pair[V]) (bool, error) {
		return scalarValue(s, pair.Left) && scalarValue(s, pair.Right) && pair.Left == pair.Right, nil
	})
}

// Different requires unequal scalar values without coercion or normalization.
func Different[V Scalar]() Rule[Pair[V]] {
	return valueRule(Spec{ID: "foundry.different"}, scalarWireTransform[V](), func(s *execution, pair Pair[V]) (bool, error) {
		return scalarValue(s, pair.Left) && scalarValue(s, pair.Right) && pair.Left != pair.Right, nil
	})
}

// Compare binds a reusable comparator to fields with the same request and
// value types. Failures use the first field's wire path; metadata retains both
// names. Custom comparators use Custom[Pair[V]] and remain server-only.
func Compare[T, V any](field, other Field[T, V], comparator Rule[Pair[V]]) Rule[T] {
	if err := field.Validate(); err != nil {
		return failed[T](err)
	}
	if err := other.Validate(); err != nil {
		return failed[T](err)
	}
	return lift(Description{Kind: CompareKind, Field: field.name, OtherField: other.name, Label: field.label, OtherLabel: other.label, LabelKey: field.labelKey, OtherLabelKey: other.labelKey}, comparator, func(s *execution, input T, path string, depth int) {
		left := field.selectValue(input)
		if !s.take(depth + 1) {
			return
		}
		right := other.selectValue(input)
		previous, previousKey, previousField := s.label, s.labelKey, s.field
		oldOther, oldOtherLabel, oldOtherKey := s.otherField, s.otherLabel, s.otherLabelKey
		s.otherField, s.otherLabel, s.otherLabelKey = other.name, other.label, other.labelKey
		defer func() { s.otherField, s.otherLabel, s.otherLabelKey = oldOther, oldOtherLabel, oldOtherKey }()
		s.label, s.labelKey, s.field = field.label, field.labelKey, field.name
		defer func() { s.label, s.labelKey, s.field = previous, previousKey, previousField }()
		comparator.run(s, Pair[V]{Left: left, Right: right}, jsonpointer.Append(path, field.name), depth+1)
	})
}
