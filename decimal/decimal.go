// Package decimal provides immutable, comparable finite decimal values without
// floating-point conversion. Values have canonical numeric equality, not a
// retained display scale. Use explicit formatting at presentation boundaries.
package decimal

import (
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

// Cmp compares numeric values, returning -1, 0 or 1.
func (d Decimal) Cmp(other Decimal) int {
	x, y, _ := aligned(d, other)
	return x.Cmp(y)
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

// Mul returns an exact product or a digit-bound error. Division is deliberately
// absent until a caller-selected scale and rounding contract is provided.
func (d Decimal) Mul(other Decimal) (Decimal, error) {
	x, y := d.coefficient(), other.coefficient()
	return fromCoefficient(x.Mul(x, y), d.Scale()+other.Scale())
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
