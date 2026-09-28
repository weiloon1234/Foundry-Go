package decimal_test

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func parse(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	v, err := decimal.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCanonicalEqualityAndExactJSON(t *testing.T) {
	for input, canonical := range map[string]string{"+000.000": "0", "-0": "0", "00042.1000": "42.1", "-000.0010": "-0.001", "9007199254740993.1234567890123456789": "9007199254740993.1234567890123456789"} {
		value := parse(t, input)
		if value.String() != canonical || value != parse(t, canonical) {
			t.Fatal("canonical value or equality changed")
		}
		data, err := json.Marshal(value)
		if err != nil || string(data) != `"`+canonical+`"` {
			t.Fatal("decimal must serialize as an exact string")
		}
		var restored decimal.Decimal
		if err := json.Unmarshal(data, &restored); err != nil || restored != value {
			t.Fatal("exact JSON round trip failed")
		}
	}
	var zero decimal.Decimal
	if !zero.IsZero() || zero.String() != "0" || zero != parse(t, "0.000") {
		t.Fatal("zero value is not canonical numeric zero")
	}
	value := decimal.FromInt64(42)
	for _, invalid := range []string{`null`, `42.1`, `"NaN"`, `"1e3"`, `{}`} {
		if err := json.Unmarshal([]byte(invalid), &value); err == nil || value != decimal.FromInt64(42) {
			t.Fatal("invalid input accepted or replaced receiver")
		}
	}
}

func TestSyntaxBoundsAndExactArithmetic(t *testing.T) {
	for _, input := range []string{"", "+", "-", ".1", "1.", "1.2.3", " 1", "1 ", "1e3", "NaN", "Infinity", "1_000", "１２", strings.Repeat("1", decimal.MaxDigits+1)} {
		if _, err := decimal.Parse(input); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid decimal accepted")
		}
	}
	x, y := parse(t, "9007199254740993.000000000000000001"), parse(t, "0.000000000000000009")
	sum, err := x.Add(y)
	if err != nil || sum.String() != "9007199254740993.00000000000000001" {
		t.Fatal("addition lost precision")
	}
	difference, err := sum.Sub(x)
	if err != nil || difference != y || x.Cmp(sum) != -1 || sum.Cmp(x) != 1 || x.Cmp(x) != 0 {
		t.Fatal("subtraction or comparison lost precision")
	}
	product, err := parse(t, "-123.45").Mul(parse(t, "0.002"))
	if err != nil || product.String() != "-0.2469" {
		t.Fatal("multiplication lost precision")
	}
	maximum := parse(t, strings.Repeat("9", decimal.MaxDigits))
	if _, err := maximum.Add(decimal.FromInt64(1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded arithmetic result accepted")
	}
	if _, err := maximum.Mul(decimal.FromInt64(10)); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded multiplication result accepted")
	}
	if zero, err := maximum.Sub(maximum); err != nil || !zero.IsZero() {
		t.Fatal("exact cancellation failed")
	}
}

func FuzzExactArithmetic(f *testing.F) {
	f.Add("12.30", "-0.002")
	f.Add("999999999999999999999.1", "0.9")
	f.Add("0", "-0")
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 128 || len(b) > 128 {
			return
		}
		x, ex := decimal.Parse(a)
		y, ey := decimal.Parse(b)
		if ex != nil || ey != nil {
			return
		}
		xr, _ := new(big.Rat).SetString(x.String())
		yr, _ := new(big.Rat).SetString(y.String())
		if x.Cmp(y) != xr.Cmp(yr) || (x == y) != (xr.Cmp(yr) == 0) {
			t.Fatal("numeric equality differs from exact rational reference")
		}
		for _, operation := range []struct {
			decimal  func(decimal.Decimal) (decimal.Decimal, error)
			rational func(*big.Rat, *big.Rat) *big.Rat
		}{
			{x.Add, new(big.Rat).Add}, {x.Sub, new(big.Rat).Sub}, {x.Mul, new(big.Rat).Mul},
		} {
			actual, err := operation.decimal(y)
			if err != nil {
				t.Fatal(err)
			}
			ar, _ := new(big.Rat).SetString(actual.String())
			if ar.Cmp(operation.rational(xr, yr)) != 0 {
				t.Fatal("arithmetic differs from exact rational reference")
			}
		}
	})
}
