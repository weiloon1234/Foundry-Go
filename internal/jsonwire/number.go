package jsonwire

import (
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

// JSON accepts exponents; the decimal package owns canonical exact equality
// and digit limits after this bounded conversion to its plain representation.
func normalizeNumber(text string) (string, error) {
	mantissa, exponent := text, int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		mantissa = text[:i]
		var err error
		exponent, err = strconv.ParseInt(text[i+1:], 10, 32)
		if err != nil || exponent > decimal.MaxDigits || exponent < -decimal.MaxDigits {
			return "", invalid()
		}
	}
	negative := strings.HasPrefix(mantissa, "-")
	if negative {
		mantissa = mantissa[1:]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	if len(whole)+len(fraction) > decimal.MaxDigits {
		return "", invalid()
	}
	digits := whole + fraction
	point := int64(len(whole)) + exponent
	var plain string
	switch {
	case point <= 0:
		if 1-point+int64(len(digits)) > decimal.MaxDigits {
			return "", invalid()
		}
		plain = "0." + strings.Repeat("0", int(-point)) + digits
	case point >= int64(len(digits)):
		if point > decimal.MaxDigits {
			return "", invalid()
		}
		plain = digits + strings.Repeat("0", int(point)-len(digits))
	default:
		plain = digits[:point] + "." + digits[point:]
	}
	if negative {
		plain = "-" + plain
	}
	number, err := decimal.Parse(plain)
	if err != nil {
		return "", invalid()
	}
	return number.String(), nil
}
