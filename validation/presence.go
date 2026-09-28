package validation

import "github.com/weiloon1234/Foundry-Go/value"

// Absent requires omission. A supplied zero, empty or null value is still
// present; use explicit value rules when those representations should be allowed.
func Absent[T any]() Rule[value.Optional[T]] {
	return valueRule(Spec{ID: "foundry.absent"}, false, func(_ *execution, input value.Optional[T]) (bool, error) { return !input.IsSet(), nil })
}

// Pointer skips nil and validates a non-nil pointed-to value. It does not infer
// wire presence: an ordinary pointer can represent both omission and null.
func Pointer[T any](rules ...Rule[T]) Rule[*T] {
	child := All(rules...)
	return lift(Description{Kind: PointerKind}, child, func(s *execution, input *T, path string, depth int) {
		if input != nil {
			child.run(s, *input, path, depth+1)
		}
	})
}

// NotNil rejects nil pointers without dereferencing them.
func NotNil[T any]() Rule[*T] {
	return valueRule(Spec{ID: "foundry.not_nil"}, false, func(_ *execution, input *T) (bool, error) { return input != nil, nil })
}
