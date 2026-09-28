package validation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestTextFormatRulesPreserveRustCharacterSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		rule           validation.Rule[string]
		valid, invalid []string
	}{
		{"prefix", validation.StartsWith("成员_"), []string{"成员_123"}, []string{"x成员_123", "成员123"}},
		{"suffix", validation.EndsWith(".PDF"), []string{"file.PDF"}, []string{"file.pdf", "file.PDF.tmp"}},
		{"alpha", validation.Alpha[string](), []string{"", "Hello", "中文", "\u0345", "Ⅳ"}, []string{"Hello1", "hello world", "a_b", "a!"}},
		{"alphanumeric", validation.AlphaNumeric[string](), []string{"", "成员123", "Ⅳ²٣"}, []string{"user@123", "a-b"}},
		{"digits", validation.Digits[string](), []string{"", "000123"}, []string{"１２３", "٣", "-1", "1.0", "1e3", " 1"}},
		{"timezone", validation.Timezone[string](), []string{"UTC", "Asia/Kuala_Lumpur", "+08:00", "-03:30"}, []string{"", "Local", "Invalid/Zone", "+24:00", "+08:60"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range tc.valid {
				if err := tc.rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
					t.Fatalf("valid input %q: %v", input, err)
				}
			}
			for _, input := range tc.invalid {
				rejection(t, tc.rule.Check(t.Context(), input, validation.DefaultLimits()))
			}
		})
	}
}

func TestTextFormatsKeepTypesBoundsAndCodecMetadata(t *testing.T) {
	t.Parallel()
	type Code string
	rule := validation.All(validation.StartsWith[Code]("00"), validation.Digits[Code]())
	if err := rule.Check(t.Context(), Code("0001"), validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	limits := validation.DefaultLimits()
	limits.ValueBytes = 3
	var bound *validation.LimitError
	if err := rule.Check(t.Context(), Code("0001"), limits); !errors.As(err, &bound) {
		t.Fatal("text format ignored byte bound", err)
	}
	if validation.StartsWith(strings.Repeat("x", 16385)).Validate() == nil {
		t.Fatal("unbounded prefix declaration accepted")
	}
	if validation.EndsWith(string([]byte{0xff})).Validate() == nil {
		t.Fatal("invalid UTF-8 suffix accepted")
	}
	requireServerOnly(t, validation.StartsWith(WireState("YES")), true)
	requireServerOnly(t, validation.Digits[string](), false)
	requireServerOnly(t, validation.Alpha[string](), true)
}
