package validation_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestExpandedTextRules(t *testing.T) {
	tests := []struct {
		name      string
		rule      validation.Rule[string]
		good, bad string
	}{
		{"ascii", validation.ASCII[string](), "hello\t", "é"},
		{"alpha_dash", validation.AlphaDash[string](), "用户_a-2", "a.b"},
		{"lowercase", validation.Lowercase[string](), "élan 12", "Élan"},
		{"uppercase", validation.Uppercase[string](), "ÉLAN 12", "Élan"},
		{"contains", validation.Contains("cat"), "a cat here", "dog"},
		{"not_contains", validation.DoesntContain("cat"), "dog", "a cat here"},
		{"not_start", validation.DoesntStartWith("admin"), "user", "administrator"},
		{"not_end", validation.DoesntEndWith(".exe"), "test.pdf", "test.exe"},
		{"not_regex", validation.NotMatches[string](`^admin`), "user", "admin1"},
		{"hex", validation.HexColor[string](), "#aBcD", "#12345"},
		{"mac", validation.MACAddress[string](), "00:11:22:33:44:55", "00:11:22:33:44:55:66:77"},
		{"ulid", validation.ULID[string](), "01arz3ndektsv4rrffq69g5fav", "81ARZ3NDEKTSV4RRFFQ69G5FAV"},
		{"length", validation.Length[string](2), "你好", "one"},
		{"length_between", validation.LengthBetween[string](2, 4), "ééé", "a"},
		{"digits_between", validation.DigitsBetween[string](2, 4), "001", "１２"},
		{"min_digits", validation.MinDigits[string](3), "001", "01"},
		{"max_digits", validation.MaxDigits[string](3), "001", "0001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.rule.Check(t.Context(), tc.good, validation.DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			rejection(t, tc.rule.Check(t.Context(), tc.bad, validation.DefaultLimits()))
			rejection(t, tc.rule.Check(t.Context(), string([]byte{255}), validation.DefaultLimits()))
			limits := validation.DefaultLimits()
			limits.ValueBytes = 1
			var exceeded *validation.LimitError
			if !errors.As(tc.rule.Check(t.Context(), "long", limits), &exceeded) {
				t.Fatal("byte limit not enforced")
			}
			desc, err := tc.rule.Description()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = desc.Normalize(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, rule := range []validation.Rule[string]{validation.LengthBetween[string](5, 1), validation.Length[string](-1), validation.NotMatches[string]("["), validation.DoesntContain(strings.Repeat("x", 20000))} {
		if rule.Validate() == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
}

func TestExpandedNumbersConsentAndComparisons(t *testing.T) {
	type units uint64
	rule := validation.Between(units(9007199254740993), units(math.MaxUint64))
	if err := rule.Check(t.Context(), math.MaxUint64, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, rule.Check(t.Context(), 9007199254740992, validation.DefaultLimits()))
	exact := validation.MultipleOf(units(3))
	if err := exact.Check(t.Context(), math.MaxUint64, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, exact.Check(t.Context(), math.MaxUint64-1, validation.DefaultLimits()))
	if validation.MultipleOf(0).Validate() == nil || validation.MultipleOf(-1).Validate() == nil || validation.Between(2, 1).Validate() == nil || validation.Between(math.NaN(), 1.0).Validate() == nil {
		t.Fatal("invalid numeric bounds accepted")
	}
	for _, tc := range []struct {
		rule        validation.Rule[validation.Pair[uint64]]
		left, right uint64
	}{
		{validation.GreaterThan[uint64](), 9007199254740993, 9007199254740992},
		{validation.GreaterOrEqual[uint64](), math.MaxUint64, math.MaxUint64},
		{validation.LessThan[uint64](), 9007199254740992, 9007199254740993},
		{validation.LessOrEqual[uint64](), 0, 0},
	} {
		if err := tc.rule.Check(t.Context(), validation.Pair[uint64]{Left: tc.left, Right: tc.right}, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	rejection(t, validation.GreaterThan[float64]().Check(t.Context(), validation.Pair[float64]{Left: math.Inf(1)}, validation.DefaultLimits()))
	for _, accepted := range []bool{true, false} {
		rule := validation.Accepted[bool]()
		if !accepted {
			rule = validation.Declined[bool]()
		}
		if err := rule.Check(t.Context(), accepted, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		rejection(t, rule.Check(t.Context(), !accepted, validation.DefaultLimits()))
	}
	a, _ := decimal.Parse("9007199254740993.001")
	b, _ := decimal.Parse("9007199254740993.002")
	if err := validation.DecimalBetween(a, b).Check(t.Context(), a, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if validation.DecimalBetween(b, a).Validate() == nil {
		t.Fatal("reversed decimal range accepted")
	}
}

func TestCollectionRulesOwnMembersAndBudget(t *testing.T) {
	members := []string{"a", "b"}
	contains := validation.ContainsItems[[]string](members...)
	members[0] = "changed"
	if err := contains.Check(t.Context(), []string{"b", "a", "a"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, contains.Check(t.Context(), []string{"a"}, validation.DefaultLimits()))
	rejection(t, validation.ExcludesItems[[]string]("a").Check(t.Context(), []string{"b", "a"}, validation.DefaultLimits()))
	if err := validation.ExcludesItems[[]string]("a").Check(t.Context(), nil, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err := validation.Items[[]int](2).Check(t.Context(), []int{1, 2}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.ItemsBetween[[]int](1, 2).Check(t.Context(), nil, validation.DefaultLimits()))
	type item struct{ SKU string }
	key := validation.DefineField("sku", func(v item) string { return v.SKU })
	unique := validation.DistinctBy[[]item](key)
	if err := unique.Check(t.Context(), []item{{"a"}, {"b"}}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, unique.Check(t.Context(), []item{{"a"}, {"a"}}, validation.DefaultLimits()))
	limits := validation.DefaultLimits()
	limits.Checks = 2
	var exceeded *validation.LimitError
	if !errors.As(contains.Check(t.Context(), []string{"a", "b"}, limits), &exceeded) {
		t.Fatal("collection escaped work cap")
	}
}

func TestPasswordRulesPreserveSecretsAndPolicy(t *testing.T) {
	options := validation.DefaultPasswordOptions()
	options.Letters = true
	options.MixedCase = true
	options.Numbers = true
	options.Symbols = true
	plain := validation.Password[string](options)
	for _, bad := range []string{"shortA1!", "long lowercase 1!", "LONG UPPERCASE 1!", "Long No Numbers!", "Long No Symbols1"} {
		rejection(t, plain.Check(t.Context(), bad, validation.DefaultLimits()))
	}
	input := "Long Password 1!"
	if err := plain.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	redacted, err := password.NewPlaintext(secret.New(input))
	if err != nil {
		t.Fatal(err)
	}
	protected := validation.PasswordValue[password.Plaintext](options)
	if err := protected.Check(t.Context(), redacted, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	info, err := protected.Description()
	if err != nil || !info.ServerOnly || strings.Contains(info.Spec.Message, input) {
		t.Fatal("unsafe password metadata", err)
	}
	if validation.Password[string](validation.PasswordOptions{}).Validate() == nil {
		t.Fatal("invalid password declaration accepted")
	}
}
