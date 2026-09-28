package validation

import (
	"math"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr | ~float32 | ~float64
}

func finite[N Number](input N) bool {
	return !math.IsNaN(float64(input)) && !math.IsInf(float64(input), 0)
}

// Min and Max compare in N itself; integers never pass through a floating-point
// comparison. Floating-point bounds and input values must be finite.
func Min[N Number](minimum N) Rule[N] {
	if !finite(minimum) {
		return failed[N](invalid("numeric bound must be finite"))
	}
	return valueRule(Spec{ID: "foundry.min", Parameters: []Parameter{scalarParameter("min", minimum)}}, false, func(_ *execution, input N) (bool, error) { return finite(input) && input >= minimum, nil })
}
func Max[N Number](maximum N) Rule[N] {
	if !finite(maximum) {
		return failed[N](invalid("numeric bound must be finite"))
	}
	return valueRule(Spec{ID: "foundry.max", Parameters: []Parameter{scalarParameter("max", maximum)}}, false, func(_ *execution, input N) (bool, error) { return finite(input) && input <= maximum, nil })
}

// DecimalMin and DecimalMax preserve the framework's exact decimal comparison.
// Their metadata uses the same string representation as decimal DTO fields.
func DecimalMin(minimum decimal.Decimal) Rule[decimal.Decimal] {
	return valueRule(Spec{ID: "foundry.decimal_min", Parameters: []Parameter{parameter("min", minimum.String())}}, false, func(_ *execution, input decimal.Decimal) (bool, error) { return input.Cmp(minimum) >= 0, nil })
}
func DecimalMax(maximum decimal.Decimal) Rule[decimal.Decimal] {
	return valueRule(Spec{ID: "foundry.decimal_max", Parameters: []Parameter{parameter("max", maximum.String())}}, false, func(_ *execution, input decimal.Decimal) (bool, error) { return input.Cmp(maximum) <= 0, nil })
}
