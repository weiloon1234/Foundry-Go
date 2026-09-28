package validation_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Contact string
type Patch struct {
	Name     Contact
	Nickname value.Optional[value.Nullable[string]]
	Tags     []string
}

func patchRules() validation.Rule[Patch] {
	name := validation.DefineField("display~/name", func(p Patch) Contact { return p.Name })
	nickname := validation.DefineField("nickname", func(p Patch) value.Optional[value.Nullable[string]] { return p.Nickname })
	tags := validation.DefineField("tags", func(p Patch) []string { return p.Tags })
	return validation.All(
		name.Rules(validation.Bail(validation.NonBlank[Contact](), validation.MinLength[Contact](3))),
		nickname.Rules(validation.Optional(validation.Nullable(validation.MinLength[string](2)))),
		tags.Rules(validation.Each[[]string](validation.NonBlank[string]())),
	)
}

func rejection(t *testing.T, err error) *validation.Errors {
	t.Helper()
	var rejected *validation.Errors
	if !errors.As(err, &rejected) {
		t.Fatalf("expected typed validation rejection, got %v", err)
	}
	return rejected
}

func TestTypedFieldsKeepPathsPresenceAndValueTypes(t *testing.T) {
	t.Parallel()
	rules := patchRules()
	for _, nickname := range []value.Optional[value.Nullable[string]]{{}, value.Set(value.Null[string]()), value.Set(value.Of("ok"))} {
		if err := rules.Check(t.Context(), Patch{Name: "member", Nickname: nickname, Tags: []string{"ok"}}, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	err := rules.Check(t.Context(), Patch{Name: " ", Nickname: value.Set(value.Of("x")), Tags: []string{"ok", " "}}, validation.DefaultLimits())
	issues := rejection(t, err).Issues()
	if len(issues) != 3 || issues[0].Path != "/display~0~1name" || issues[1].Path != "/nickname" || issues[2].Path != "/tags/1" {
		t.Fatalf("field paths/order: %+v", issues)
	}
	issues[0].Message = "changed"
	if rejection(t, err).Issues()[0].Message == "changed" {
		t.Fatal("caller changed retained diagnostics")
	}
	if strings.Contains(err.Error(), "member") {
		t.Fatal("input appeared in error text")
	}
}

func TestPresenceAndNullAreSeparate(t *testing.T) {
	t.Parallel()
	rule := validation.Bail(validation.Present[value.Nullable[int]](), validation.Optional(validation.NotNull[int]()))
	for _, input := range []value.Optional[value.Nullable[int]]{{}, value.Set(value.Null[int]())} {
		rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits()))
	}
	if err := rule.Check(t.Context(), value.Set(value.Of(0)), validation.DefaultLimits()); err != nil {
		t.Fatal("present zero rejected", err)
	}
}

type Amount uint64

func (Amount) MarshalJSON() ([]byte, error) {
	panic("numeric metadata must not invoke an application codec")
}

func TestNumericBoundsAndMetadataRemainExact(t *testing.T) {
	t.Parallel()
	const bound Amount = 9007199254740993
	rule := validation.Min(bound)
	if err := rule.Check(t.Context(), bound, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, rule.Check(t.Context(), bound-1, validation.DefaultLimits()))
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if string(info.Spec.Parameters[0].Value) != "9007199254740993" {
		t.Fatal("numeric declaration lost exactness")
	}
	for _, input := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		rejection(t, validation.Min(0.0).Check(t.Context(), input, validation.DefaultLimits()))
	}
	if validation.Max(math.Inf(1)).Validate() == nil {
		t.Fatal("infinite declaration accepted")
	}
	minimum, err := decimal.Parse("9007199254740993.000000000001")
	if err != nil {
		t.Fatal(err)
	}
	below, err := decimal.Parse("9007199254740993.000000000000")
	if err != nil {
		t.Fatal(err)
	}
	decimals := validation.DecimalMin(minimum)
	if err := decimals.Check(t.Context(), minimum, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, decimals.Check(t.Context(), below, validation.DefaultLimits()))
}

func TestDescriptionsAndOverridesAreOwned(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	spec := validation.Spec{ID: "app.contact", Message: "Use a contact address.", Parameters: []validation.Parameter{{Name: "region", Value: json.RawMessage(`"local"`)}}}
	rule := validation.Custom(spec, func(context.Context, string) (bool, error) { calls.Add(1); return false, nil })
	spec.Parameters[0].Value[1] = 'x'
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if !info.ServerOnly || string(info.Spec.Parameters[0].Value) != `"local"` {
		t.Fatal("custom metadata was not copied")
	}
	info.Spec.Parameters[0].Value[1] = 'z'
	info.Spec.Message = "changed"
	for range 10000 {
		rule = rule.WithMessage("Use the declared contact format.")
	}
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	issues := rejection(t, rule.Check(t.Context(), "private-input", validation.DefaultLimits())).Issues()
	if calls.Load() != 1 || len(issues) != 1 || issues[0].Message != "Use the declared contact format." {
		t.Fatal("message overrides changed execution")
	}
	all := validation.All(validation.MinLength[string](1), rule)
	info, err = all.Description()
	if err != nil || !info.ServerOnly {
		t.Fatal("composite hid its server-only rule")
	}
	info.Children[1].Spec.Message = "changed"
	again, _ := all.Description()
	if again.Children[1].Spec.Message == "changed" {
		t.Fatal("nested metadata was shared")
	}
}

func TestCustomDefinitionConflictsAndZeroValuesFailEarly(t *testing.T) {
	t.Parallel()
	check := func(context.Context, string) (bool, error) { return true, nil }
	first := validation.Custom(validation.Spec{ID: "app.shared", Message: "Declared message."}, check)
	second := validation.Custom(validation.Spec{ID: "app.shared", Message: "Declared message."}, check)
	if !errors.Is(validation.All(first, second).Validate(), fault.Duplicate) {
		t.Fatal("conflicting custom IDs accepted")
	}
	if err := validation.All(first, first.WithMessage("Another field message.")).Validate(); err != nil {
		t.Fatal(err)
	}
	var zero validation.Rule[string]
	var field validation.Field[Patch, string]
	for _, err := range []error{zero.Validate(), field.Rules(validation.NonBlank[string]()).Validate(), validation.Custom[string](validation.Spec{ID: "app.nil", Message: "No callback."}, nil).Validate(), validation.Custom(validation.Spec{ID: "foundry.fake", Message: "Reserved."}, check).Validate(), validation.Matches[string]("[").Validate()} {
		if err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
}

type hostileError struct{}

func (hostileError) Error() string { panic("private-error-format") }

func TestCallbackFailuresStayOwnedAndPrivate(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "returned", "cancel-panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rule := validation.Custom(validation.Spec{ID: "app.failure", Message: "Public message."}, func(context.Context, string) (bool, error) {
				switch mode {
				case "returned":
					return false, hostileError{}
				case "goexit":
					runtime.Goexit()
				case "cancel-panic":
					cancel()
				}
				panic("private-panic")
			})
			err := rule.Check(ctx, "private-input", validation.DefaultLimits())
			if !errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe callback result: %v", err)
			}
			var rejected *validation.Errors
			if errors.As(err, &rejected) {
				t.Fatal("infrastructure failure became a validation rejection")
			}
		})
	}
}

func TestCancellationWaitsForCallbackAndSkipsLaterRules(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	rule := validation.All(
		validation.Custom(validation.Spec{ID: "app.wait", Message: "Wait."}, func(context.Context, string) (bool, error) { close(entered); <-release; return true, nil }),
		validation.Custom(validation.Spec{ID: "app.later", Message: "Later."}, func(context.Context, string) (bool, error) {
			t.Error("later rule ran after cancellation")
			return true, nil
		}),
	)
	go func() { done <- rule.Check(ctx, "value", validation.DefaultLimits()) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("callback was abandoned")
	default:
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not finish")
	}
}

func TestValidationBoundsAndUnicode(t *testing.T) {
	t.Parallel()
	if err := validation.MinLength[string](2).Check(t.Context(), "你好", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.MinLength[string](3).Check(t.Context(), "你好", validation.DefaultLimits()))
	limits := validation.DefaultLimits()
	limits.ValueBytes = 4
	var limited *validation.LimitError
	if err := validation.NonBlank[string]().Check(t.Context(), "longer", limits); !errors.As(err, &limited) {
		t.Fatal("text bound was not applied", err)
	}
	limits = validation.DefaultLimits()
	limits.Checks = 2
	if err := validation.Each[[]string](validation.NonBlank[string]()).Check(t.Context(), []string{"one", "two"}, limits); !errors.As(err, &limited) {
		t.Fatal("work bound was not applied", err)
	}
	limits = validation.DefaultLimits()
	limits.Issues = 1
	err := validation.Each[[]string](validation.NonBlank[string]()).Check(t.Context(), []string{"", ""}, limits)
	rejected := rejection(t, err)
	if len(rejected.Issues()) != 1 || !rejected.Truncated() {
		t.Fatal("issue cap was not explicit")
	}
}

func TestCrossFieldComparatorKeepsDeclaredNames(t *testing.T) {
	t.Parallel()
	type registration struct{ Password, Confirmation string }
	password := validation.DefineField("password", func(r registration) string { return r.Password })
	confirmation := validation.DefineField("passwordConfirmation", func(r registration) string { return r.Confirmation })
	rule := validation.Compare(confirmation, password, validation.Same[string]().WithMessage("The confirmation must match."))
	if err := rule.Check(t.Context(), registration{"same", "same"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	issues := rejection(t, rule.Check(t.Context(), registration{"secret", "different"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/passwordConfirmation" {
		t.Fatal("comparator lost its declared field")
	}
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerOnly || info.OtherField != "password" || info.Kind != validation.CompareKind {
		t.Fatal("comparator metadata lost the related field")
	}
}

func TestComparisonReusesCustomRulesAndStopsBetweenSelectors(t *testing.T) {
	t.Parallel()
	type request struct{ A, B, C string }
	a := validation.DefineField("a", func(r request) string { return r.A })
	b := validation.DefineField("b", func(r request) string { return r.B })
	c := validation.DefineField("c", func(r request) string { return r.C })
	comparator := validation.Custom(validation.Spec{ID: "app.same_fold", Message: "These fields must match."}, func(_ context.Context, pair validation.Pair[string]) (bool, error) {
		return strings.EqualFold(pair.Left, pair.Right), nil
	})
	rule := validation.All(validation.Compare(a, b, comparator), validation.Compare(b, c, comparator))
	if err := rule.Check(t.Context(), request{"YES", "yes", "Yes"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := validation.DefineField("a", func(r request) string { cancel(); return r.A })
	second := validation.DefineField("b", func(r request) string { t.Error("selector ran after cancellation"); return r.B })
	err := validation.Compare(first, second, comparator).Check(ctx, request{}, validation.DefaultLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDeclarationsRejectInvalidMetadataAndExcessiveNesting(t *testing.T) {
	t.Parallel()
	check := func(context.Context, string) (bool, error) { return true, nil }
	for _, parameters := range [][]validation.Parameter{
		{{Name: "bound", Value: json.RawMessage(`{"duplicate":1,"duplicate":2}`)}},
		{{Name: "bound", Value: json.RawMessage(`1`)}, {Name: "bound", Value: json.RawMessage(`2`)}},
		{{Name: "bound", Value: json.RawMessage(`NaN`)}},
	} {
		rule := validation.Custom(validation.Spec{ID: "app.bound", Message: "Bounded.", Parameters: parameters}, check)
		if rule.Validate() == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	rule := validation.NonBlank[string]()
	for range 65 {
		rule = validation.All(rule)
	}
	if rule.Validate() == nil {
		t.Fatal("unbounded declaration depth accepted")
	}
	large := validation.NonBlank[string]().WithMessage(strings.Repeat("x", 16384))
	rules := make([]validation.Rule[string], 65)
	for i := range rules {
		rules[i] = large
	}
	if validation.All(rules...).Validate() == nil {
		t.Fatal("unbounded aggregate metadata accepted")
	}
}
