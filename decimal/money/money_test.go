package money_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func amount(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	value, err := decimal.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func of(t *testing.T, text string, code money.Currency) money.Money {
	t.Helper()
	value, err := money.New(amount(t, text), code)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCurrencyCodesAndMinorUnits(t *testing.T) {
	for code, minor := range map[string]int{"USD": 2, "EUR": 2, "MYR": 2, "JPY": 0, "KRW": 0, "BHD": 3, "KWD": 3, "CLF": 4, "VES": 2, "ZWG": 2, "UYW": 4} {
		c, err := money.ParseCurrency(code)
		if err != nil {
			t.Fatal(code, err)
		}
		if units, err := c.MinorUnits(); err != nil || units != minor {
			t.Fatal(code, units, err)
		}
	}
	for _, code := range []string{"", "usd", "US", "USDD", "AAA", "XAU", "XXX", "XTS", "U$D"} {
		if _, err := money.ParseCurrency(code); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid currency accepted", code)
		}
	}
	if _, err := money.Currency("usd").MinorUnits(); err == nil {
		t.Fatal("a cast bypassed currency validation")
	}
}

func TestMoneyNeverRoundsImplicitly(t *testing.T) {
	if _, err := money.New(amount(t, "1.005"), "USD"); !errors.Is(err, fault.Invalid) {
		t.Fatal("excess minor digits accepted")
	}
	if _, err := money.New(amount(t, "1.5"), "JPY"); !errors.Is(err, fault.Invalid) {
		t.Fatal("fractional yen accepted")
	}
	rounded, err := money.NewRounded(amount(t, "1.005"), "USD", decimal.HalfEven)
	if err != nil || rounded.Amount().String() != "1" {
		t.Fatal(rounded, err)
	}
	rounded, err = money.NewRounded(amount(t, "1.005"), "USD", decimal.HalfUp)
	if err != nil || rounded.Amount().String() != "1.01" {
		t.Fatal(rounded, err)
	}
	cents, err := money.FromMinor(-1234, "USD")
	if err != nil || cents.Amount().String() != "-12.34" {
		t.Fatal(cents, err)
	}
	if units, err := cents.MinorUnits(); err != nil || units != -1234 {
		t.Fatal(units, err)
	}
	dinar, err := money.FromMinor(1500, "KWD")
	if err != nil || dinar.Amount().String() != "1.5" {
		t.Fatal(dinar, err)
	}
	var zero money.Money
	if zero.Validate() == nil {
		t.Fatal("zero Money has no currency")
	}
}

func TestSameCurrencyArithmetic(t *testing.T) {
	price, tax := of(t, "19.99", "USD"), of(t, "1.60", "USD")
	total, err := price.Add(tax)
	if err != nil || total != of(t, "21.59", "USD") {
		t.Fatal(total, err)
	}
	change, err := of(t, "20", "USD").Sub(total)
	if err != nil || change.Amount().String() != "-1.59" || change.Neg().Sign() != 1 || change.Abs() != change.Neg() {
		t.Fatal(change, err)
	}
	if order, err := price.Cmp(tax); err != nil || order != 1 {
		t.Fatal(order, err)
	}
	for _, check := range []func() error{
		func() error { _, err := price.Add(of(t, "1", "EUR")); return err },
		func() error { _, err := price.Sub(of(t, "1", "EUR")); return err },
		func() error { _, err := price.Cmp(of(t, "1", "EUR")); return err },
		func() error { _, err := price.Add(money.Money{}); return err },
	} {
		if err := check(); !errors.Is(err, fault.Invalid) {
			t.Fatal("mixed or invalid currency arithmetic accepted", err)
		}
	}
	taxed, err := price.Multiply(amount(t, "0.0825"), decimal.HalfUp)
	if err != nil || taxed.Amount().String() != "1.65" {
		t.Fatal(taxed, err)
	}
	if _, err := price.Multiply(amount(t, "2"), 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("multiplication without a rounding mode accepted")
	}
}

func TestAllocationPreservesEveryMinorUnit(t *testing.T) {
	parts, err := of(t, "100", "USD").Split(3)
	if err != nil || len(parts) != 3 || parts[0].Amount().String() != "33.34" || parts[1].Amount().String() != "33.33" || parts[2].Amount().String() != "33.33" {
		t.Fatal(parts, err)
	}
	parts, err = of(t, "0.05", "USD").Allocate(3, 7)
	if err != nil || parts[0].Amount().String() != "0.02" || parts[1].Amount().String() != "0.03" {
		t.Fatal(parts, err)
	}
	parts, err = of(t, "-10", "JPY").Allocate(1, 1, 1)
	if err != nil || parts[0].Amount().String() != "-4" || parts[1].Amount().String() != "-3" || parts[2].Amount().String() != "-3" {
		t.Fatal(parts, err)
	}
	parts, err = of(t, "10", "USD").Allocate(0, 1)
	if err != nil || !parts[0].IsZero() || parts[1] != of(t, "10", "USD") {
		t.Fatal(parts, err)
	}
	for _, ratios := range [][]int64{{}, {0, 0}, {1, -1}} {
		if _, err := of(t, "1", "USD").Allocate(ratios...); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid allocation accepted", ratios)
		}
	}
	if _, err := of(t, "1", "USD").Split(money.MaxAllocationParts + 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded split accepted")
	}
}

func TestMoneyJSONIsExactAndStrict(t *testing.T) {
	data, err := json.Marshal(of(t, "12.3", "USD"))
	if err != nil || string(data) != `{"amount":"12.30","currency":"USD"}` {
		t.Fatal(string(data), err)
	}
	var restored money.Money
	if err := json.Unmarshal(data, &restored); err != nil || restored != of(t, "12.3", "USD") {
		t.Fatal(restored, err)
	}
	original := restored
	for _, invalid := range []string{
		`null`, `{}`, `{"amount":"1"}`, `{"amount":12.3,"currency":"USD"}`, `{"amount":"1","currency":"usd"}`,
		`{"amount":"1.001","currency":"USD"}`, `{"amount":"1","currency":"USD","extra":1}`,
		`{"amount":"1","amount":"2","currency":"USD"}`, `{"amount":null,"currency":"USD"}`, `"12.30 USD"`,
	} {
		if err := json.Unmarshal([]byte(invalid), &restored); err == nil || restored != original {
			t.Fatal("invalid money JSON accepted or replaced receiver", invalid)
		}
	}
	if err := restored.UnmarshalJSON([]byte(`{"amount":"1","currency":"USD"}x`)); err == nil {
		t.Fatal("trailing data accepted")
	}
	if _, err := json.Marshal(money.Money{}); err == nil {
		t.Fatal("zero Money serialized")
	}
	var code money.Currency
	if err := json.Unmarshal([]byte(`"EUR"`), &code); err != nil || code != "EUR" {
		t.Fatal(code, err)
	}
	if err := json.Unmarshal([]byte(`null`), &code); err == nil {
		t.Fatal("null currency accepted")
	}
}
