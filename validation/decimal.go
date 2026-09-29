package validation

import (
	"math/big"
	"strings"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

// DecimalGreaterThan and its siblings compare two exact decimal fields through
// Compare, for example a maximum price against a minimum price. They never
// convert through floating point; failures use the first field's path.
func DecimalGreaterThan() Rule[Pair[decimal.Decimal]] {
	return decimalPair("foundry.greater_than", func(order int) bool { return order > 0 })
}
func DecimalGreaterOrEqual() Rule[Pair[decimal.Decimal]] {
	return decimalPair("foundry.greater_or_equal", func(order int) bool { return order >= 0 })
}
func DecimalLessThan() Rule[Pair[decimal.Decimal]] {
	return decimalPair("foundry.less_than", func(order int) bool { return order < 0 })
}
func DecimalLessOrEqual() Rule[Pair[decimal.Decimal]] {
	return decimalPair("foundry.less_or_equal", func(order int) bool { return order <= 0 })
}

func decimalPair(id RuleID, accept func(int) bool) Rule[Pair[decimal.Decimal]] {
	return valueRule(Spec{ID: id}, true, func(_ *execution, pair Pair[decimal.Decimal]) (bool, error) {
		return accept(pair.Left.Cmp(pair.Right)), nil
	})
}

// DecimalMaxPlaces limits significant fractional digits. Decimals are canonical,
// so 1.50 has one place; validate raw text when trailing zeros are significant.
func DecimalMaxPlaces(maximum int) Rule[decimal.Decimal] {
	if maximum < 0 || maximum > decimal.MaxDigits {
		return failed[decimal.Decimal](invalid("decimal place bound must be between 0 and the decimal digit bound"))
	}
	return valueRule(Spec{ID: "foundry.decimal_places", Parameters: []Parameter{parameter("places", maximum)}}, true, func(_ *execution, input decimal.Decimal) (bool, error) {
		return input.Scale() <= maximum, nil
	})
}

// DecimalMultipleOf requires an exact integer quotient, for example a quantity
// in 0.25 steps. The divisor must be positive; comparison is exact at any scale.
func DecimalMultipleOf(divisor decimal.Decimal) Rule[decimal.Decimal] {
	if divisor.Sign() <= 0 {
		return failed[decimal.Decimal](invalid("multiple-of divisor must be positive"))
	}
	return valueRule(Spec{ID: "foundry.multiple_of", Parameters: []Parameter{parameter("divisor", divisor.String())}}, true, func(_ *execution, input decimal.Decimal) (bool, error) {
		scale := max(input.Scale(), divisor.Scale())
		remainder := new(big.Int).Rem(decimalCoefficient(input, scale), decimalCoefficient(divisor, scale))
		return remainder.Sign() == 0, nil
	})
}

// decimalCoefficient returns value × 10^scale for a scale at least as large as
// the value's own, using the canonical text representation.
func decimalCoefficient(value decimal.Decimal, scale int) *big.Int {
	whole, fraction, _ := strings.Cut(value.String(), ".")
	digits := whole + fraction + strings.Repeat("0", scale-len(fraction))
	coefficient, _ := new(big.Int).SetString(digits, 10)
	return coefficient
}
