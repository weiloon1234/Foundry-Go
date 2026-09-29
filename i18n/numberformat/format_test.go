package numberformat_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/numberformat"
)

func formatter(t *testing.T, locale i18n.LocaleID) *numberformat.Formatter {
	t.Helper()
	f, err := numberformat.New(locale)
	if err != nil {
		t.Fatal(locale, err)
	}
	return f
}

func exact(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	value, err := decimal.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func cash(t *testing.T, text string, code money.Currency) money.Money {
	t.Helper()
	value, err := money.New(exact(t, text), code)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNumbersUseLocaleSeparatorsDigitsAndSigns(t *testing.T) {
	for _, tc := range []struct {
		locale i18n.LocaleID
		input  string
		want   string
	}{
		{"en", "1234567.5", "1,234,567.5"},
		{"en", "-0.001", "-0.001"},
		{"en", "999", "999"},
		{"en", "1000", "1,000"},
		{"de", "1234567.5", "1.234.567,5"},
		{"fr", "1234567.5", "1 234 567,5"},
		{"de-CH", "1234567.5", "1’234’567.5"},
		{"hi", "1234567", "12,34,567"},
		{"hi", "123456789.25", "12,34,56,789.25"},
		{"ar", "-1234.5", "؜-١٬٢٣٤٫٥"},
		{"ar-u-nu-latn", "1234.5", "1,234.5"},
		{"sv", "-12", "−12"},
		{"en", "12345678901234567890.123456789", "12,345,678,901,234,567,890.123456789"},
	} {
		if got := formatter(t, tc.locale).Number(exact(t, tc.input)); got != tc.want {
			t.Fatalf("%s %s: got %q want %q", tc.locale, tc.input, got, tc.want)
		}
	}
}

func TestFixedAndPercentRoundExplicitly(t *testing.T) {
	en, de, tr := formatter(t, "en"), formatter(t, "de"), formatter(t, "tr")
	for _, tc := range []struct {
		got  func() (string, error)
		want string
	}{
		{func() (string, error) { return en.Fixed(exact(t, "2.5"), 2, decimal.HalfUp) }, "2.50"},
		{func() (string, error) { return en.Fixed(exact(t, "-1234.565"), 2, decimal.HalfEven) }, "-1,234.56"},
		{func() (string, error) { return de.Fixed(exact(t, "1234.5"), 0, decimal.HalfUp) }, "1.235"},
		{func() (string, error) { return en.Percent(exact(t, "0.125"), 1, decimal.HalfUp) }, "12.5%"},
		{func() (string, error) { return en.Percent(exact(t, "-0.5"), 0, decimal.HalfUp) }, "-50%"},
		{func() (string, error) { return de.Percent(exact(t, "0.125"), 1, decimal.HalfUp) }, "12,5 %"},
		{func() (string, error) { return tr.Percent(exact(t, "0.5"), 0, decimal.HalfUp) }, "%50"},
		{func() (string, error) { return en.Percent(exact(t, "12.3456"), 0, decimal.Down) }, "1,234%"},
	} {
		if got, err := tc.got(); err != nil || got != tc.want {
			t.Fatalf("got %q (%v) want %q", got, err, tc.want)
		}
	}
	if _, err := en.Fixed(exact(t, "1"), 2, 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("rounding mode is required")
	}
	if _, err := en.Percent(exact(t, "1"), -1, decimal.HalfUp); !errors.Is(err, fault.Invalid) {
		t.Fatal("negative scale accepted")
	}
}

func TestMoneyUsesMinorUnitsSymbolsAndPlacement(t *testing.T) {
	for _, tc := range []struct {
		locale  i18n.LocaleID
		amount  money.Money
		display numberformat.CurrencyDisplay
		want    string
	}{
		{"en", cash(t, "1234.5", "USD"), numberformat.Symbol, "$1,234.50"},
		{"en", cash(t, "-5", "USD"), numberformat.Symbol, "-$5.00"},
		{"en", cash(t, "12", "CHF"), numberformat.Code, "CHF 12.00"},
		{"en", cash(t, "1235", "JPY"), numberformat.Symbol, "¥1,235"},
		{"en", cash(t, "1.5", "KWD"), numberformat.Code, "KWD 1.500"},
		{"en", cash(t, "3", "VES"), numberformat.Symbol, "VES 3.00"},
		{"de", cash(t, "1234.5", "EUR"), numberformat.Symbol, "1.234,50 €"},
		{"de", cash(t, "-1234.5", "EUR"), numberformat.Symbol, "-1.234,50 €"},
		{"de-CH", cash(t, "12", "CHF"), numberformat.Code, "CHF 12.00"},
		{"nl", cash(t, "5", "EUR"), numberformat.Symbol, "€ 5,00"},
		{"fr", cash(t, "10", "USD"), numberformat.NarrowSymbol, "10,00 $"},
		{"hi", cash(t, "1234567", "INR"), numberformat.Code, "INR 12,34,567.00"},
	} {
		got, err := formatter(t, tc.locale).Money(tc.amount, tc.display)
		if err != nil || got != tc.want {
			t.Fatalf("%s %v: got %q (%v) want %q", tc.locale, tc.amount, got, err, tc.want)
		}
	}
	en := formatter(t, "en")
	suffix, err := en.WithCurrencyPlacement(numberformat.SymbolAfterSpaced)
	if err != nil || en.Placement() != numberformat.SymbolBefore || suffix.Placement() != numberformat.SymbolAfterSpaced {
		t.Fatal("placement override must copy the formatter", err)
	}
	if got, err := suffix.Money(cash(t, "1", "USD"), numberformat.Symbol); err != nil || got != "1.00 $" {
		t.Fatal(got, err)
	}
	if _, err := en.WithCurrencyPlacement(0); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid placement accepted")
	}
	if _, err := en.Money(money.Money{}, numberformat.Symbol); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero Money formatted")
	}
	if _, err := en.Money(cash(t, "1", "USD"), 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("currency display is required")
	}
}

func TestLocalesAndConcurrentUse(t *testing.T) {
	for _, locale := range []i18n.LocaleID{"", "EN", "not a locale"} {
		if _, err := numberformat.New(locale); err == nil {
			t.Fatal("invalid locale accepted", locale)
		}
	}
	for _, locale := range []i18n.LocaleID{"en", "en-GB", "ms", "id", "zh-Hans", "ja", "ko", "th", "vi", "es", "pt", "pt-PT", "it", "nl", "ru", "pl", "tr", "ar", "fa", "he", "bn", "mr", "my", "sw", "und"} {
		f := formatter(t, locale)
		if f.Locale() != locale || f.Number(exact(t, "-1234567.5")) == "" {
			t.Fatal("locale formatter unavailable", locale)
		}
	}
	de, price := formatter(t, "de"), cash(t, "9.99", "EUR")
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := de.Money(price, numberformat.Symbol); err != nil || got != "9,99 €" {
				t.Error(got, err)
			}
		}()
	}
	wg.Wait()
}
