package decimal_test

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var roundingModes = []decimal.RoundingMode{decimal.HalfUp, decimal.HalfEven, decimal.Down, decimal.Up, decimal.Floor, decimal.Ceiling}

func TestRoundingModesAtTiesAndSigns(t *testing.T) {
	// Expected results per mode: HalfUp, HalfEven, Down, Up, Floor, Ceiling.
	for input, expected := range map[string][6]string{
		"2.5":    {"3", "2", "2", "3", "2", "3"},
		"3.5":    {"4", "4", "3", "4", "3", "4"},
		"-2.5":   {"-3", "-2", "-2", "-3", "-3", "-2"},
		"-3.5":   {"-4", "-4", "-3", "-4", "-4", "-3"},
		"2.4":    {"2", "2", "2", "3", "2", "3"},
		"-2.6":   {"-3", "-3", "-2", "-3", "-3", "-2"},
		"0.4":    {"0", "0", "0", "1", "0", "1"},
		"-0.4":   {"0", "0", "0", "-1", "-1", "0"},
		"-0.5":   {"-1", "0", "0", "-1", "-1", "0"},
		"7":      {"7", "7", "7", "7", "7", "7"},
		"9.5000": {"10", "10", "9", "10", "9", "10"},
	} {
		for i, mode := range roundingModes {
			rounded, err := parse(t, input).Round(0, mode)
			if err != nil || rounded.String() != expected[i] {
				t.Fatal(input, mode, rounded, err)
			}
		}
	}
	rounded, err := parse(t, "1.2345").Round(2, decimal.HalfEven)
	if err != nil || rounded.String() != "1.23" {
		t.Fatal(rounded, err)
	}
	unchanged, err := parse(t, "1.2").Round(4, decimal.Down)
	if err != nil || unchanged.String() != "1.2" {
		t.Fatal("rounding must not change a value that already fits", unchanged, err)
	}
}

func TestDivisionRequiresExplicitPolicy(t *testing.T) {
	quotient, err := decimal.FromInt64(10).Div(decimal.FromInt64(3), 4, decimal.HalfUp)
	if err != nil || quotient.String() != "3.3333" {
		t.Fatal(quotient, err)
	}
	quotient, err = decimal.FromInt64(-2).Div(decimal.FromInt64(3), 2, decimal.HalfUp)
	if err != nil || quotient.String() != "-0.67" {
		t.Fatal(quotient, err)
	}
	quotient, err = parse(t, "1").Div(parse(t, "-0.008"), 0, decimal.HalfEven)
	if err != nil || quotient.String() != "-125" {
		t.Fatal(quotient, err)
	}
	quotient, err = parse(t, "0.125").Div(parse(t, "0.5"), 1, decimal.HalfEven)
	if err != nil || quotient.String() != "0.2" {
		t.Fatal(quotient, err)
	}
	for _, check := range []func() (decimal.Decimal, error){
		func() (decimal.Decimal, error) { return decimal.FromInt64(1).Div(decimal.Decimal{}, 2, decimal.HalfUp) },
		func() (decimal.Decimal, error) { return decimal.FromInt64(1).Div(decimal.FromInt64(3), 2, 0) },
		func() (decimal.Decimal, error) { return decimal.FromInt64(1).Div(decimal.FromInt64(3), -1, decimal.Up) },
		func() (decimal.Decimal, error) {
			return decimal.FromInt64(1).Div(decimal.FromInt64(3), decimal.MaxDigits+1, decimal.Up)
		},
		func() (decimal.Decimal, error) { return decimal.FromInt64(1).Round(0, decimal.RoundingMode(99)) },
		func() (decimal.Decimal, error) {
			return parse(t, strings.Repeat("9", decimal.MaxDigits)).Div(parse(t, "0.1"), 0, decimal.Down)
		},
	} {
		if _, err := check(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid division or rounding accepted", err)
		}
	}
}

func TestSignHelpersAggregatesAndFixedText(t *testing.T) {
	value := parse(t, "-12.5")
	if value.Sign() != -1 || value.Neg().String() != "12.5" || value.Abs().String() != "12.5" || value.Neg().Neg() != value {
		t.Fatal("sign helpers changed magnitude")
	}
	var zero decimal.Decimal
	if zero.Neg() != zero || zero.Abs() != zero || zero.Sign() != 0 {
		t.Fatal("zero must not gain a sign")
	}
	if decimal.Min(parse(t, "2"), parse(t, "-3"), parse(t, "1")).String() != "-3" || decimal.Max(parse(t, "2"), parse(t, "10.01"), parse(t, "10")).String() != "10.01" {
		t.Fatal("min/max selected the wrong value")
	}
	total, err := decimal.Sum(parse(t, "0.1"), parse(t, "0.2"), parse(t, "-0.3"), parse(t, "5.005"))
	if err != nil || total.String() != "5.005" {
		t.Fatal(total, err)
	}
	if empty, err := decimal.Sum(); err != nil || !empty.IsZero() {
		t.Fatal(empty, err)
	}
	scaled, err := decimal.Scaled(-1234, 2)
	if err != nil || scaled.String() != "-12.34" {
		t.Fatal(scaled, err)
	}
	if _, err := decimal.Scaled(1, -1); !errors.Is(err, fault.Invalid) {
		t.Fatal("negative scale accepted")
	}
	for input, expected := range map[string]string{"0": "0.00", "-1.5": "-1.50", "12": "12.00", "3.25": "3.25"} {
		if text, err := parse(t, input).Fixed(2); err != nil || text != expected {
			t.Fatal(input, text, err)
		}
	}
	if _, err := parse(t, "1.005").Fixed(2); !errors.Is(err, fault.Invalid) {
		t.Fatal("fixed formatting must not round implicitly")
	}
}

func TestCompareDoesNotAllocate(t *testing.T) {
	values := []decimal.Decimal{parse(t, "-100.5"), parse(t, "-100.25"), parse(t, "-0.001"), {}, parse(t, "0.5"), parse(t, "0.51"), parse(t, "9"), parse(t, "10"), parse(t, "10.000001")}
	for i := range values {
		for j := range values {
			if got, want := values[i].Cmp(values[j]), cmpInts(i, j); got != want {
				t.Fatal(values[i], values[j], got, want)
			}
		}
	}
	x, y := parse(t, "123456789.123456789"), parse(t, "123456789.12345679")
	if allocations := testing.AllocsPerRun(100, func() { _ = x.Cmp(y) }); allocations != 0 {
		t.Fatal("Cmp allocated", allocations)
	}
}

func cmpInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func FuzzRoundedDivision(f *testing.F) {
	f.Add("10", "3", uint8(2), uint8(1))
	f.Add("-2.5", "1", uint8(0), uint8(2))
	f.Add("0.0001", "-0.3", uint8(5), uint8(6))
	f.Fuzz(func(t *testing.T, a, b string, scale, rawMode uint8) {
		if len(a) > 64 || len(b) > 64 || scale > 40 {
			return
		}
		x, ex := decimal.Parse(a)
		y, ey := decimal.Parse(b)
		mode := decimal.RoundingMode(rawMode%6 + 1)
		if ex != nil || ey != nil || y.IsZero() {
			return
		}
		actual, err := x.Div(y, int(scale), mode)
		if err != nil {
			t.Fatal(err)
		}
		xr, _ := new(big.Rat).SetString(x.String())
		yr, _ := new(big.Rat).SetString(y.String())
		expected := referenceRound(new(big.Rat).Quo(xr, yr), int(scale), mode)
		ar, _ := new(big.Rat).SetString(actual.String())
		if ar.Cmp(expected) != 0 {
			t.Fatal("rounded division differs from rational reference", x, y, scale, mode, actual, expected.FloatString(int(scale)))
		}
	})
}

// referenceRound independently rounds an exact rational at scale.
func referenceRound(value *big.Rat, scale int, mode decimal.RoundingMode) *big.Rat {
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(factor))
	floor := new(big.Int).Div(scaled.Num(), scaled.Denom()) // Euclidean: floor for positive denominators.
	fraction := new(big.Rat).Sub(scaled, new(big.Rat).SetInt(floor))
	result := new(big.Int).Set(floor)
	if fraction.Sign() != 0 {
		up := new(big.Int).Add(floor, big.NewInt(1))
		half := fraction.Cmp(big.NewRat(1, 2))
		switch mode {
		case decimal.Ceiling:
			result = up
		case decimal.Floor:
		case decimal.Up:
			if scaled.Sign() > 0 {
				result = up
			}
		case decimal.Down:
			if scaled.Sign() < 0 {
				result = up
			}
		case decimal.HalfUp:
			if half > 0 || half == 0 && scaled.Sign() > 0 {
				result = up
			}
		case decimal.HalfEven:
			if half > 0 || half == 0 && floor.Bit(0) == 1 {
				result = up
			}
		}
	}
	return new(big.Rat).SetFrac(result, factor)
}
