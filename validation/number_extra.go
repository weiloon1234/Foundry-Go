package validation

import "github.com/weiloon1234/Foundry-Go/decimal"

// Integer restricts exact modulo checks to ordinary or named integer values.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

func Between[N Number](minimum, maximum N) Rule[N] {
	if minimum > maximum {
		return failed[N](invalid("numeric bounds are reversed"))
	}
	return Bail(Min(minimum), Max(maximum))
}

// MultipleOf uses exact integer remainder. The divisor must be positive.
func MultipleOf[N Integer](divisor N) Rule[N] {
	if divisor <= 0 {
		return failed[N](invalid("multiple-of divisor must be positive"))
	}
	return valueRule(Spec{ID: "foundry.multiple_of", Parameters: []Parameter{scalarParameter("divisor", divisor)}}, true, func(_ *execution, input N) (bool, error) { return input%divisor == 0, nil })
}
func DecimalBetween(minimum, maximum decimal.Decimal) Rule[decimal.Decimal] {
	if minimum.Cmp(maximum) > 0 {
		return failed[decimal.Decimal](invalid("decimal bounds are reversed"))
	}
	return Bail(DecimalMin(minimum), DecimalMax(maximum))
}
func GreaterThan[N Number]() Rule[Pair[N]] {
	return numericPair[N]("foundry.greater_than", func(a, b N) bool { return a > b })
}
func GreaterOrEqual[N Number]() Rule[Pair[N]] {
	return numericPair[N]("foundry.greater_or_equal", func(a, b N) bool { return a >= b })
}
func LessThan[N Number]() Rule[Pair[N]] {
	return numericPair[N]("foundry.less_than", func(a, b N) bool { return a < b })
}
func LessOrEqual[N Number]() Rule[Pair[N]] {
	return numericPair[N]("foundry.less_or_equal", func(a, b N) bool { return a <= b })
}
func numericPair[N Number](id RuleID, check func(N, N) bool) Rule[Pair[N]] {
	return valueRule(Spec{ID: id}, true, func(_ *execution, input Pair[N]) (bool, error) {
		return finite(input.Left) && finite(input.Right) && check(input.Left, input.Right), nil
	})
}

// Accepted and Declined use actual booleans. Text coercion belongs to decoding.
func Accepted[B ~bool]() Rule[B] { return booleanRule[B](true) }
func Declined[B ~bool]() Rule[B] { return booleanRule[B](false) }
func booleanRule[B ~bool](want bool) Rule[B] {
	spec := Spec{ID: "foundry.accepted"}
	if !want {
		spec = Spec{ID: "foundry.declined"}
	}
	return valueRule(spec, true, func(_ *execution, input B) (bool, error) { return bool(input) == want, nil })
}

// Confirmed compares explicitly selected fields; it never guesses a suffix.
func Confirmed[T any, V Scalar](field, confirmation Field[T, V]) Rule[T] {
	return Compare(field, confirmation, Same[V]())
}
