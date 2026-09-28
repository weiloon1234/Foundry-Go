package http

import (
	"math"
	"reflect"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// PathFloat retains float32, float64 and their named types in URL parameters.
type PathFloat interface{ ~float32 | ~float64 }

// FloatPath accepts finite decimal numbers, including scientific notation and
// signed zero. Parsing rounds to the concrete float width using strconv's
// nearest-value rules; underflow can round to zero. It rejects overflow,
// whitespace, hexadecimal literals, digit separators, NaN and infinity.
// Format emits the shortest decimal that round trips at that width.
// Use a text codec such as decimal.Decimal when decimal values must stay exact.
func FloatPath[V PathFloat]() PathCodec[V] {
	bits := reflect.TypeFor[V]().Bits()
	return nativeURLCodec[V](floatPathCodec[V]{bits: bits}, contract.Type{Kind: contract.NumberKind, Bits: uint8(bits)}, FloatURLSyntax)
}

type floatPathCodec[V PathFloat] struct{ bits int }

func (c floatPathCodec[V]) Parse(text string) (V, error) {
	var zero V
	// ParseFloat also accepts Go hexadecimal literals and digit separators.
	// Limit its alphabet to decimal/scientific notation; strconv owns the
	// syntax, rounding and range checks within that alphabet.
	for i := range len(text) {
		b := text[i]
		if b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.' || b == 'e' || b == 'E' {
			continue
		}
		return zero, invalidURLFloat()
	}
	parsed, err := strconv.ParseFloat(text, c.bits)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return zero, invalidURLFloat()
	}
	return V(parsed), nil
}

func (c floatPathCodec[V]) Format(value V) (string, error) {
	number := float64(value)
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return "", invalidURLFloat()
	}
	return strconv.FormatFloat(number, 'g', -1, c.bits), nil
}

func invalidURLFloat() error {
	return fault.New(fault.Invalid, "invalid floating-point URL parameter")
}
