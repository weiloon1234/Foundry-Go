package validation

import "github.com/weiloon1234/Foundry-Go/value"

// Required requires a supplied, nonempty Optional value. Supplied numeric zero
// and false pass. Additional same-type rules run only after that requirement
// succeeds and bail on the first rejection. For nullable input use
// RequiredNullable; ordinary fields use NonEmpty without inferring presence.
func Required[T any](rules ...Rule[T]) Rule[value.Optional[T]] {
	check := nativeEmptyCheck[T]()
	if check.err != nil {
		return failed[value.Optional[T]](check.err)
	}
	base := valueRule(requiredSpec(check.parameters()), check.serverOnly, func(s *execution, input value.Optional[T]) (bool, error) {
		selected, supplied := input.Get()
		if !supplied {
			return false, nil
		}
		empty, valid := check.inspect(s, selected)
		return valid && !empty, nil
	})
	if len(rules) == 0 {
		return base
	}
	return Bail(base, Optional(Bail(rules...)))
}

// RequiredNullable distinguishes omission, explicit null and supplied T. Null
// and empty content reject; zero numbers and false remain valid supplied values.
// Additional T rules run only on the nonempty, non-null value and bail on failure.
func RequiredNullable[T any](rules ...Rule[T]) Rule[value.Optional[value.Nullable[T]]] {
	check := nativeEmptyCheck[T]()
	if check.err != nil {
		return failed[value.Optional[value.Nullable[T]]](check.err)
	}
	base := valueRule(requiredSpec(check.parameters()), check.serverOnly, func(s *execution, input value.Optional[value.Nullable[T]]) (bool, error) {
		nullable, supplied := input.Get()
		if !supplied {
			return false, nil
		}
		selected, nonnull := nullable.Get()
		if !nonnull {
			return false, nil
		}
		empty, valid := check.inspect(s, selected)
		return valid && !empty, nil
	})
	if len(rules) == 0 {
		return base
	}
	return Bail(base, Optional(Nullable(Bail(rules...))))
}

func requiredSpec(parameters []Parameter) Spec {
	return Spec{ID: "foundry.required", Parameters: parameters}
}

// Prohibited accepts omission or empty content. Unlike Absent, supplied blank
// text and empty collections pass. Supplied numeric zero and false reject.
// Use ProhibitedNullable when explicit null is a representable input state.
func Prohibited[T any]() Rule[value.Optional[T]] {
	check := nativeEmptyCheck[T]()
	if check.err != nil {
		return failed[value.Optional[T]](check.err)
	}
	return valueRule(prohibitedSpec(check.parameters()), check.serverOnly, func(s *execution, input value.Optional[T]) (bool, error) {
		selected, supplied := input.Get()
		if !supplied {
			return true, nil
		}
		empty, valid := check.inspect(s, selected)
		return valid && empty, nil
	})
}

// ProhibitedNullable also accepts explicit null, while preserving the distinction
// from omission and a nonempty T. It does not mutate or unset supplied values.
func ProhibitedNullable[T any]() Rule[value.Optional[value.Nullable[T]]] {
	check := nativeEmptyCheck[T]()
	if check.err != nil {
		return failed[value.Optional[value.Nullable[T]]](check.err)
	}
	return valueRule(prohibitedSpec(check.parameters()), check.serverOnly, func(s *execution, input value.Optional[value.Nullable[T]]) (bool, error) {
		nullable, supplied := input.Get()
		if !supplied {
			return true, nil
		}
		selected, nonnull := nullable.Get()
		if !nonnull {
			return true, nil
		}
		empty, valid := check.inspect(s, selected)
		return valid && empty, nil
	})
}

func prohibitedSpec(parameters []Parameter) Spec {
	return Spec{ID: "foundry.prohibited", Parameters: parameters}
}
