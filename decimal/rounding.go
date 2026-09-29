package decimal

import (
	"math/big"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// RoundingMode selects how discarded digits change a rounded result. The zero
// value is invalid, so every rounding or division call states its policy.
type RoundingMode uint8

const (
	// HalfUp rounds to the nearest value; ties move away from zero.
	HalfUp RoundingMode = iota + 1
	// HalfEven rounds to the nearest value; ties choose the even neighbor.
	HalfEven
	// Down truncates toward zero.
	Down
	// Up moves any discarded nonzero digits away from zero.
	Up
	// Floor rounds toward negative infinity.
	Floor
	// Ceiling rounds toward positive infinity.
	Ceiling
)

func (m RoundingMode) Validate() error {
	if m < HalfUp || m > Ceiling {
		return fault.New(fault.Invalid, "invalid decimal rounding mode")
	}
	return nil
}

func (m RoundingMode) String() string {
	switch m {
	case HalfUp:
		return "half_up"
	case HalfEven:
		return "half_even"
	case Down:
		return "down"
	case Up:
		return "up"
	case Floor:
		return "floor"
	case Ceiling:
		return "ceiling"
	}
	return "invalid"
}

func invalidScale() error {
	return fault.New(fault.Invalid, "decimal scale must be between 0 and MaxDigits")
}

// Round returns the value with at most scale fractional digits. Values that
// already fit are returned unchanged; nothing is padded because display scale
// is not retained. Use Fixed to present a fixed number of digits.
func (d Decimal) Round(scale int, mode RoundingMode) (Decimal, error) {
	if err := mode.Validate(); err != nil {
		return Decimal{}, err
	}
	if scale < 0 || scale > MaxDigits {
		return Decimal{}, invalidScale()
	}
	if d.Scale() <= scale {
		return d, nil
	}
	return divideRounded(d.coefficient(), power(d.Scale()-scale), scale, mode)
}

// Div returns d / divisor rounded to at most scale fractional digits with an
// explicit mode. Division by zero, an invalid mode or scale, and results beyond
// MaxDigits fail. Operands never change.
func (d Decimal) Div(divisor Decimal, scale int, mode RoundingMode) (Decimal, error) {
	if err := mode.Validate(); err != nil {
		return Decimal{}, err
	}
	if scale < 0 || scale > MaxDigits {
		return Decimal{}, invalidScale()
	}
	if divisor.IsZero() {
		return Decimal{}, fault.New(fault.Invalid, "decimal division by zero")
	}
	if d.IsZero() {
		return Decimal{}, nil
	}
	// d/divisor = (a·10^-sa)/(b·10^-sb); the scaled quotient is a·10^(scale+sb-sa)/b.
	numerator, denominator := d.coefficient(), divisor.coefficient()
	if exponent := scale + divisor.Scale() - d.Scale(); exponent > 0 {
		numerator.Mul(numerator, power(exponent))
	} else if exponent < 0 {
		denominator.Mul(denominator, power(-exponent))
	}
	return divideRounded(numerator, denominator, scale, mode)
}

// divideRounded returns numerator/denominator as a coefficient at scale. The
// operands are owned by the caller and may be modified.
func divideRounded(numerator, denominator *big.Int, scale int, mode RoundingMode) (Decimal, error) {
	if denominator.Sign() < 0 {
		numerator.Neg(numerator)
		denominator.Neg(denominator)
	}
	negative := numerator.Sign() < 0
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() != 0 && roundsAway(quotient, remainder, denominator, negative, mode) {
		if negative {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return fromCoefficient(quotient, scale)
}

// roundsAway reports whether a truncated quotient with a nonzero remainder
// moves one unit away from zero.
func roundsAway(quotient, remainder, denominator *big.Int, negative bool, mode RoundingMode) bool {
	switch mode {
	case Up:
		return true
	case Down:
		return false
	case Ceiling:
		return !negative
	case Floor:
		return negative
	}
	twice := new(big.Int).Abs(remainder)
	half := twice.Lsh(twice, 1).Cmp(denominator)
	if mode == HalfUp {
		return half >= 0
	}
	return half > 0 || half == 0 && quotient.Bit(0) == 1
}

// Fixed formats the value with exactly scale fractional digits, padding zeros.
// It never rounds: values with more fractional digits fail, so callers choose a
// rounding mode with Round first.
func (d Decimal) Fixed(scale int) (string, error) {
	if scale < 0 || scale > MaxDigits {
		return "", invalidScale()
	}
	current := d.Scale()
	if current > scale {
		return "", fault.New(fault.Invalid, "decimal has more fractional digits than the fixed scale")
	}
	text := d.String()
	if scale == current {
		return text, nil
	}
	var builder strings.Builder
	builder.Grow(len(text) + scale - current + 1)
	builder.WriteString(text)
	if current == 0 {
		builder.WriteByte('.')
	}
	for range scale - current {
		builder.WriteByte('0')
	}
	return builder.String(), nil
}
