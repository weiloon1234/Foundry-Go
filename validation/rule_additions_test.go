package validation_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func decimalValue(t *testing.T, text string) decimal.Decimal {
	t.Helper()
	value, err := decimal.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type priceRange struct{ Minimum, Maximum decimal.Decimal }

func TestDecimalFieldComparisonsPlacesAndMultiples(t *testing.T) {
	t.Parallel()
	minimum := validation.DefineField("minimum", func(input priceRange) decimal.Decimal { return input.Minimum })
	maximum := validation.DefineField("maximum", func(input priceRange) decimal.Decimal { return input.Maximum }).WithLabel("Maximum price")
	rule := validation.Compare(maximum, minimum, validation.DecimalGreaterThan())
	huge := decimalValue(t, "9007199254740993.000000000000000000001")
	if err := rule.Check(t.Context(), priceRange{Minimum: decimalValue(t, "9007199254740993"), Maximum: huge}, validation.DefaultLimits()); err != nil {
		t.Fatal("exact decimal comparison lost precision", err)
	}
	issues := rejection(t, rule.Check(t.Context(), priceRange{Minimum: huge, Maximum: huge}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/maximum" || issues[0].Message != "Maximum price must be greater than minimum." {
		t.Fatalf("decimal comparison: %+v", issues)
	}
	for _, tc := range []struct {
		rule  validation.Rule[validation.Pair[decimal.Decimal]]
		left  string
		right string
		valid bool
	}{
		{validation.DecimalGreaterOrEqual(), "1.50", "1.5", true},
		{validation.DecimalLessThan(), "-0.1", "0", true},
		{validation.DecimalLessOrEqual(), "2", "1.999", false},
	} {
		err := tc.rule.Check(t.Context(), validation.Pair[decimal.Decimal]{Left: decimalValue(t, tc.left), Right: decimalValue(t, tc.right)}, validation.DefaultLimits())
		if (err == nil) != tc.valid {
			t.Fatal(tc.left, tc.right, err)
		}
	}

	places := validation.DecimalMaxPlaces(2)
	if err := places.Check(t.Context(), decimalValue(t, "10.250"), validation.DefaultLimits()); err != nil {
		t.Fatal("canonical trailing zero counted", err)
	}
	issues = rejection(t, places.Check(t.Context(), decimalValue(t, "0.125"), validation.DefaultLimits())).Issues()
	if issues[0].Code != "foundry.decimal_places" || issues[0].Message != "This field must have at most 2 decimal places." {
		t.Fatalf("places: %+v", issues)
	}
	step := validation.DecimalMultipleOf(decimalValue(t, "0.25"))
	for _, text := range []string{"0", "1.75", "-2.5", "123456789012345678901234567890.25"} {
		if err := step.Check(t.Context(), decimalValue(t, text), validation.DefaultLimits()); err != nil {
			t.Fatal(text, err)
		}
	}
	rejection(t, step.Check(t.Context(), decimalValue(t, "1.3"), validation.DefaultLimits()))
	for _, bad := range []validation.Rule[decimal.Decimal]{validation.DecimalMaxPlaces(-1), validation.DecimalMultipleOf(decimal.Decimal{}), validation.DecimalMultipleOf(decimalValue(t, "-1"))} {
		if bad.Validate() == nil {
			t.Fatal("invalid decimal declaration accepted")
		}
	}
}

type Locale string
type labelSet map[Locale]string
type EncodedKey string

func (k EncodedKey) MarshalText() ([]byte, error) { return []byte(strings.ToUpper(string(k))), nil }

func TestMapKeyAndValueRulesUseDeterministicEntryPaths(t *testing.T) {
	t.Parallel()
	labels := validation.DefineField("labels", func(input labelSet) labelSet { return input })
	rule := validation.All(
		labels.Rules(validation.EachKey[labelSet](validation.OneOf[Locale]("en", "ms"))),
		labels.Rules(validation.EachValue[labelSet](validation.NonBlank[string]())),
	)
	if err := rule.Check(t.Context(), labelSet{"en": "Hello", "ms": "Helo"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		issues := rejection(t, rule.Check(t.Context(), labelSet{"zz": "x", "en": " ", "a/b": "", "ms": ""}, validation.DefaultLimits())).Issues()
		paths := []string{}
		for _, issue := range issues {
			paths = append(paths, issue.Path)
		}
		if strings.Join(paths, ",") != "/labels/a~1b,/labels/zz,/labels/a~1b,/labels/en,/labels/ms" {
			t.Fatalf("map entry order: %v", paths)
		}
	}
	counts := validation.EachValue[map[int]int](validation.Min(1))
	issues := rejection(t, counts.Check(t.Context(), map[int]int{10: 0, -2: 0, 3: 1}, validation.DefaultLimits())).Issues()
	if len(issues) != 2 || issues[0].Path != "/-2" || issues[1].Path != "/10" {
		t.Fatalf("integer keys: %+v", issues)
	}
	info, err := rule.Description()
	if err != nil || !info.ServerOnly || info.Children[0].Children[0].Children[0].Kind != validation.EachKeyKind {
		t.Fatalf("map metadata: %+v %v", info, err)
	}
	if _, err := info.Normalize(); err != nil {
		t.Fatal(err)
	}
	if validation.EachKey[map[EncodedKey]string](validation.NonBlank[EncodedKey]()).Validate() == nil {
		t.Fatal("custom-coded map key accepted")
	}
	limits := validation.DefaultLimits()
	limits.Checks = 3
	var bounded *validation.LimitError
	if err := counts.Check(t.Context(), map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, limits); !errors.As(err, &bounded) {
		t.Fatal("map traversal ignored the work bound", err)
	}
}

func TestDateFormatAndCaseInsensitiveDistinct(t *testing.T) {
	t.Parallel()
	format := validation.DateFormat[string]("02/01/2006")
	if err := format.Check(t.Context(), "28/09/2026", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"2026-09-28", "28/9/2026", "31/02/2026", " 28/09/2026"} {
		rejection(t, format.Check(t.Context(), text, validation.DefaultLimits()))
	}
	info, err := format.Description()
	if err != nil || !info.ServerOnly || string(info.Spec.Parameters[0].Value) != `"02/01/2006"` {
		t.Fatalf("format metadata: %+v %v", info, err)
	}
	issues := rejection(t, format.Check(t.Context(), "bad", validation.DefaultLimits())).Issues()
	if issues[0].Message != "This field must match the format 02/01/2006." {
		t.Fatalf("format message: %q", issues[0].Message)
	}
	for _, bad := range []string{"", "  "} {
		if validation.DateFormat[string](bad).Validate() == nil {
			t.Fatal("invalid layout accepted")
		}
	}
	distinct := validation.DistinctIgnoringCase[[]string]()
	if err := distinct.Check(t.Context(), []string{"Go", "Rust"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]string{{"Tag", "tag"}, {"STRASSE", "straße"}} {
		rejection(t, distinct.Check(t.Context(), input, validation.DefaultLimits()))
	}
	info, _ = distinct.Description()
	var ignore bool
	if !info.ServerOnly || json.Unmarshal(info.Spec.Parameters[0].Value, &ignore) != nil || !ignore {
		t.Fatalf("distinct metadata: %+v", info)
	}
}

type quota struct{ Used, Limit int }

func TestDynamicRulesAndHooksChooseMessagesAndPathsAtCheckTime(t *testing.T) {
	t.Parallel()
	rule := validation.Dynamic(validation.Spec{ID: "app.quota", Message: "Quota exceeded."}, func(_ context.Context, input quota) (validation.Rejection, error) {
		switch {
		case input.Used > input.Limit+10:
			return validation.Reject(""), nil
		case input.Used > input.Limit:
			return validation.Reject("Only " + strings.Repeat("I", input.Limit) + " allowed."), nil
		}
		return validation.Rejection{}, nil
	})
	if err := rule.Check(t.Context(), quota{Used: 1, Limit: 2}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	issues := rejection(t, rule.Check(t.Context(), quota{Used: 3, Limit: 2}, validation.DefaultLimits())).Issues()
	if issues[0].Code != "app.quota" || issues[0].Message != "Only II allowed." {
		t.Fatalf("dynamic message: %+v", issues)
	}
	issues = rejection(t, rule.Check(t.Context(), quota{Used: 30, Limit: 2}, validation.DefaultLimits())).Issues()
	if issues[0].Message != "Quota exceeded." {
		t.Fatalf("declared fallback: %+v", issues)
	}

	hook := validation.Hook(validation.Spec{ID: "app.line_total", Message: "Line totals disagree."}, func(_ context.Context, input order, report *validation.Report) error {
		for i, line := range input.Lines {
			if line.Product == "bad" {
				report.Add("lines", strconv.Itoa(i), "product_id")
			}
		}
		report.AddRejection(validation.Reject("Order rejected."))
		return nil
	})
	issues = rejection(t, hook.Check(t.Context(), order{Lines: []orderLine{{"ok"}, {"bad"}}}, validation.DefaultLimits())).Issues()
	if len(issues) != 2 || issues[0].Path != "/lines/1/product_id" || issues[0].Code != "app.line_total" || issues[1].Path != "" || issues[1].Message != "Order rejected." {
		t.Fatalf("hook issues: %+v", issues)
	}
	limits := validation.DefaultLimits()
	limits.Issues = 1
	capped := rejection(t, hook.Check(t.Context(), order{Lines: []orderLine{{"bad"}, {"bad"}}}, limits))
	if len(capped.Issues()) != 1 || !capped.Truncated() {
		t.Fatal("hook ignored the issue cap")
	}
	private := errors.New("private")
	failing := validation.Hook(validation.Spec{ID: "app.fail", Message: "Failed."}, func(context.Context, string, *validation.Report) error { return private })
	if err := failing.Check(t.Context(), "", validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("hook failure was not an execution failure", err)
	}
	var retained *validation.Report
	escaped := validation.Hook(validation.Spec{ID: "app.retain", Message: "Retained."}, func(_ context.Context, _ string, report *validation.Report) error {
		retained = report
		return nil
	})
	if err := escaped.Check(t.Context(), "", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	retained.Add("late")
	invalidText := validation.Dynamic(validation.Spec{ID: "app.bad_text", Message: "Bad."}, func(context.Context, string) (validation.Rejection, error) {
		return validation.Reject("bad\x00text"), nil
	})
	if err := invalidText.Check(t.Context(), "", validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("invalid dynamic text accepted", err)
	}
	for _, bad := range []validation.Rule[string]{
		validation.Dynamic[string](validation.Spec{ID: "foundry.fake", Message: "Reserved."}, func(context.Context, string) (validation.Rejection, error) { return validation.Rejection{}, nil }),
		validation.Dynamic[string](validation.Spec{ID: "app.nil", Message: "Nil."}, nil),
		validation.Hook[string](validation.Spec{ID: "app.nil", Message: "Nil."}, nil),
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid dynamic declaration accepted")
		}
	}
}

type imageFile struct{ width, height int }

type imageSizes struct{ calls int }

func (*imageSizes) Validate() error { return nil }
func (m *imageSizes) Measure(_ context.Context, file imageFile) (validation.ImageSize, bool, error) {
	m.calls++
	if file.width < 0 {
		return validation.ImageSize{}, false, errors.New("private read failure")
	}
	return validation.ImageSize{Width: file.width, Height: file.height}, file.width != 0, nil
}

func TestImageDimensionsUseTheInjectedMeasurer(t *testing.T) {
	t.Parallel()
	measurer := &imageSizes{}
	rule := validation.Dimensions[imageFile](measurer, validation.DimensionConstraints{MinWidth: 100, MaxHeight: 1000, RatioWidth: 16, RatioHeight: 9})
	if err := rule.Check(t.Context(), imageFile{1600, 900}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, file := range []imageFile{{0, 0}, {96, 54}, {1920, 1081}, {3200, 1800}} {
		issues := rejection(t, rule.Check(t.Context(), file, validation.DefaultLimits())).Issues()
		if issues[0].Code != "foundry.image_dimensions" {
			t.Fatalf("dimensions: %+v", issues)
		}
	}
	if err := rule.Check(t.Context(), imageFile{-1, 0}, validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("measurement failure was not an execution failure", err)
	}
	info, err := rule.Description()
	if err != nil || !info.ServerOnly || len(info.Spec.Parameters) != 4 {
		t.Fatalf("dimension metadata: %+v %v", info, err)
	}
	for _, constraints := range []validation.DimensionConstraints{{}, {MinWidth: -1}, {MinWidth: 10, MaxWidth: 5}, {RatioWidth: 4}} {
		if validation.Dimensions[imageFile](measurer, constraints).Validate() == nil {
			t.Fatal("invalid constraints accepted", constraints)
		}
	}
	if validation.Dimensions[imageFile](nil, validation.DimensionConstraints{Width: 1}).Validate() == nil {
		t.Fatal("missing measurer accepted")
	}
}
