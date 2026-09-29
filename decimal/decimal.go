// Package decimal provides immutable, comparable finite decimal values without
// floating-point conversion. Values have canonical numeric equality, not a
// retained display scale. Use explicit formatting at presentation boundaries.
package decimal

import (
	"cmp"
	"math/big"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

// MaxDigits bounds input digits and arithmetic results. This is a framework
// resource bound, not a claim to cover PostgreSQL's entire numeric range.
const MaxDigits = 4096

// Decimal is an exact finite decimal. Its zero value is numeric zero. Private
// canonical storage makes == agree with numeric equality, including signed zero.
type Decimal struct{ text string }

// Parse accepts optional +/-, digits and an optional fractional part. Exponents,
// whitespace, NaN and infinity are rejected. At least one digit is required on
// each side of a decimal point. Leading/trailing zeros are normalized.
func Parse(text string) (Decimal, error) {
	if len(text) == 0 || len(text) > MaxDigits+2 {
		return Decimal{}, invalid()
	}
	negative := text[0] == '-'
	if text[0] == '+' || negative {
		text = text[1:]
	}
	whole, fraction, point := strings.Cut(text, ".")
	if whole == "" || (point && fraction == "") || len(whole)+len(fraction) > MaxDigits {
		return Decimal{}, invalid()
	}
	for _, digits := range []string{whole, fraction} {
		for i := range len(digits) {
			if digits[i] < '0' || digits[i] > '9' {
				return Decimal{}, invalid()
			}
		}
	}
	whole, fraction = strings.TrimLeft(whole, "0"), strings.TrimRight(fraction, "0")
	if whole == "" && fraction == "" {
		return Decimal{}, nil
	}
	if whole == "" {
		whole = "0"
	}
	if fraction != "" {
		whole += "." + fraction
	}
	if negative {
		whole = "-" + whole
	}
	return Decimal{text: strings.Clone(whole)}, nil
}

func invalid() error { return fault.New(fault.Invalid, "invalid decimal syntax or digit bound") }

// FromInt64 converts an integer exactly, without a parse failure path.
func FromInt64(value int64) Decimal {
	if value == 0 {
		return Decimal{}
	}
	return Decimal{text: strconv.FormatInt(value, 10)}
}

// Scaled returns coefficient × 10^-scale exactly, for example Scaled(1234, 2)
// is 12.34. It is the exact bridge from integer minor units.
func Scaled(coefficient int64, scale int) (Decimal, error) {
	if scale < 0 || scale > MaxDigits {
		return Decimal{}, invalidScale()
	}
	return fromCoefficient(big.NewInt(coefficient), scale)
}

func (d Decimal) IsZero() bool { return d.text == "" }
func (d Decimal) String() string {
	if d.IsZero() {
		return "0"
	}
	return d.text
}

// Scale reports the number of fractional digits in the canonical value.
func (d Decimal) Scale() int {
	_, fraction, _ := strings.Cut(d.text, ".")
	return len(fraction)
}

func (d Decimal) coefficient() *big.Int {
	value, _ := new(big.Int).SetString(strings.ReplaceAll(d.String(), ".", ""), 10)
	return value
}

func aligned(a, b Decimal) (*big.Int, *big.Int, int) {
	scale := max(a.Scale(), b.Scale())
	x, y := a.coefficient(), b.coefficient()
	if n := scale - a.Scale(); n > 0 {
		x.Mul(x, power(n))
	}
	if n := scale - b.Scale(); n > 0 {
		y.Mul(y, power(n))
	}
	return x, y, scale
}

func power(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// Cmp compares numeric values, returning -1, 0 or 1. It compares canonical
// digits directly and does not allocate.
func (d Decimal) Cmp(other Decimal) int {
	sign, otherSign := d.Sign(), other.Sign()
	if sign != otherSign {
		return cmp.Compare(sign, otherSign)
	}
	if sign == 0 {
		return 0
	}
	magnitude := compareMagnitude(strings.TrimPrefix(d.text, "-"), strings.TrimPrefix(other.text, "-"))
	if sign < 0 {
		return -magnitude
	}
	return magnitude
}

// compareMagnitude orders unsigned canonical text. Whole parts have no leading
// zeros except a lone "0", and fractions have no trailing zeros, so length then
// lexical order decide the whole part and lexical order decides the fraction.
func compareMagnitude(a, b string) int {
	aWhole, aFraction, _ := strings.Cut(a, ".")
	bWhole, bFraction, _ := strings.Cut(b, ".")
	if len(aWhole) != len(bWhole) {
		return cmp.Compare(len(aWhole), len(bWhole))
	}
	if result := strings.Compare(aWhole, bWhole); result != 0 {
		return result
	}
	return strings.Compare(aFraction, bFraction)
}

// Sign returns -1, 0 or 1.
func (d Decimal) Sign() int {
	switch {
	case d.text == "":
		return 0
	case d.text[0] == '-':
		return -1
	}
	return 1
}

// Neg returns the additive inverse. Negating zero returns zero.
func (d Decimal) Neg() Decimal {
	switch d.Sign() {
	case 0:
		return d
	case -1:
		return Decimal{text: d.text[1:]}
	}
	return Decimal{text: "-" + d.text}
}

// Abs returns the magnitude.
func (d Decimal) Abs() Decimal {
	if d.Sign() < 0 {
		return Decimal{text: d.text[1:]}
	}
	return d
}

// Min returns the numerically smallest value, preferring the earliest on ties.
func Min(first Decimal, others ...Decimal) Decimal {
	result := first
	for _, value := range others {
		if value.Cmp(result) < 0 {
			result = value
		}
	}
	return result
}

// Max returns the numerically largest value, preferring the earliest on ties.
func Max(first Decimal, others ...Decimal) Decimal {
	result := first
	for _, value := range others {
		if value.Cmp(result) > 0 {
			result = value
		}
	}
	return result
}

// Add returns an exact sum or a digit-bound error. Receivers never change.
func (d Decimal) Add(other Decimal) (Decimal, error) {
	x, y, scale := aligned(d, other)
	return fromCoefficient(x.Add(x, y), scale)
}

// Sub returns an exact difference or a digit-bound error.
func (d Decimal) Sub(other Decimal) (Decimal, error) {
	x, y, scale := aligned(d, other)
	return fromCoefficient(x.Sub(x, y), scale)
}

// Mul returns an exact product or a digit-bound error. Division requires an
// explicit result scale and rounding mode; see Div.
func (d Decimal) Mul(other Decimal) (Decimal, error) {
	x, y := d.coefficient(), other.coefficient()
	return fromCoefficient(x.Mul(x, y), d.Scale()+other.Scale())
}

// Sum returns the exact total of values, or zero for no values. Intermediate
// values use one aligned coefficient; only the final result is digit bounded.
func Sum(values ...Decimal) (Decimal, error) {
	scale := 0
	for _, value := range values {
		scale = max(scale, value.Scale())
	}
	total := new(big.Int)
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		coefficient := value.coefficient()
		if n := scale - value.Scale(); n > 0 {
			coefficient.Mul(coefficient, power(n))
		}
		total.Add(total, coefficient)
	}
	return fromCoefficient(total, scale)
}

func fromCoefficient(coefficient *big.Int, scale int) (Decimal, error) {
	if coefficient.Sign() == 0 {
		return Decimal{}, nil
	}
	negative := coefficient.Sign() < 0
	text := strings.TrimPrefix(coefficient.String(), "-")
	// Normalize before applying the result bound: excess trailing zeros can
	// disappear in an exact operation without losing any numerical information.
	for scale > 0 && text[len(text)-1] == '0' {
		text = text[:len(text)-1]
		scale--
	}
	if max(len(text), scale+1) > MaxDigits {
		return Decimal{}, invalid()
	}
	if scale >= len(text) {
		text = strings.Repeat("0", scale-len(text)+1) + text
	}
	if scale > 0 {
		text = text[:len(text)-scale] + "." + text[len(text)-scale:]
	}
	if negative {
		text = "-" + text
	}
	return Parse(text)
}

func (d Decimal) MarshalText() ([]byte, error) { return []byte(d.String()), nil }
func (d *Decimal) UnmarshalText(data []byte) error {
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
func (d *Decimal) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, Parse)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
