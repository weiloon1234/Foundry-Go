package validation_test

import (
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestMembershipPreservesNativeValuesAndOwnedMetadata(t *testing.T) {
	t.Parallel()
	const first Amount = 9007199254740993
	rule := validation.OneOf(first, first+1)
	if err := rule.Check(t.Context(), first+1, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, rule.Check(t.Context(), first-1, validation.DefaultLimits()))
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if string(info.Spec.Parameters[0].Value) != "[9007199254740993,9007199254740994]" {
		t.Fatal("membership lost integer precision")
	}
	values := []string{"one", "two"}
	stringsRule := validation.OneOf(values...)
	values[0] = "changed"
	if err := stringsRule.Check(t.Context(), "one", validation.DefaultLimits()); err != nil {
		t.Fatal("caller changed membership", err)
	}
	for _, invalid := range []validation.Rule[float64]{validation.OneOf(math.NaN()), validation.OneOf(1.0, 1.0), validation.NotOneOf[float64]()} {
		if invalid.Validate() == nil {
			t.Fatal("invalid membership declaration accepted")
		}
	}
	rejection(t, validation.NotOneOf(1.0).Check(t.Context(), math.NaN(), validation.DefaultLimits()))
}

type WireState string

func (v WireState) MarshalJSON() ([]byte, error) { return json.Marshal(strings.ToLower(string(v))) }
func (v *WireState) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*v = WireState(strings.ToUpper(text))
	return nil
}

type PanicState string

func (PanicState) MarshalJSON() ([]byte, error) { panic("private enum codec detail") }

type ExitState string

func (ExitState) MarshalJSON() ([]byte, error) { runtime.Goexit(); return nil, nil }

func TestEnumRulesUseActualWireCasesAndOwnCodecFailures(t *testing.T) {
	t.Parallel()
	descriptor := enum.Describe("app/models", "State", enum.Case[WireState]{Name: "Yes", Value: "YES"})
	rule := validation.Enum(descriptor)
	if err := rule.Check(t.Context(), WireState("YES"), validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, rule.Check(t.Context(), WireState("yes"), validation.DefaultLimits()))
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if string(info.Spec.Parameters[1].Value) != `["yes"]` {
		t.Fatal("enum rule copied underlying values instead of wire cases")
	}
	for _, err := range []error{
		validation.Enum(enum.Describe("app", "State", enum.Case[PanicState]{Name: "Value", Value: "x"})).Validate(),
		validation.Enum(enum.Describe("app", "State", enum.Case[ExitState]{Name: "Value", Value: "x"})).Validate(),
	} {
		if !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe enum failure: %v", err)
		}
	}
}

func TestCollectionRulesBoundWorkAndRetainNativeEquality(t *testing.T) {
	t.Parallel()
	type Labels []string
	rule := validation.All(validation.MinItems[Labels](1), validation.MaxItems[Labels](3), validation.Distinct[Labels]())
	if err := rule.Check(t.Context(), Labels{"a", "A"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, input := range []Labels{nil, {"a", "a"}, {"a", "b", "c", "d"}} {
		rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits()))
	}
	limits := validation.DefaultLimits()
	limits.Checks = 2
	var bound *validation.LimitError
	err := validation.Distinct[Labels]().Check(t.Context(), Labels{"a", "b"}, limits)
	if !errors.As(err, &bound) {
		t.Fatal("distinct work was unbounded", err)
	}
	rejection(t, validation.Distinct[[]float64]().Check(t.Context(), []float64{math.NaN()}, validation.DefaultLimits()))
}
