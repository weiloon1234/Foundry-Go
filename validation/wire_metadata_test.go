package validation_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type decodedText string

func (*decodedText) UnmarshalText([]byte) error { panic("metadata invoked decoder") }

type encodedItems []string

func (encodedItems) MarshalJSON() ([]byte, error) { panic("metadata invoked encoder") }

type encodedByte byte

func (encodedByte) MarshalJSON() ([]byte, error) { panic("metadata invoked element encoder") }

func requireServerOnly[T any](t *testing.T, rule validation.Rule[T], expected bool) {
	t.Helper()
	info, err := rule.Description()
	if err != nil || info.ServerOnly != expected {
		t.Fatalf("server-only metadata: got %v, want %v, error %v", info.ServerOnly, expected, err)
	}
}

func TestNativeRulesDetectWireTransformsWithoutInvokingCodecs(t *testing.T) {
	t.Parallel()
	requireServerOnly(t, validation.OneOf(WireState("YES")), true)
	requireServerOnly(t, validation.NonBlank[WireState](), true)
	requireServerOnly(t, validation.Same[WireState](), true)
	requireServerOnly(t, validation.Different[WireState](), true)
	requireServerOnly(t, validation.NonBlank[decodedText](), true)
	requireServerOnly(t, validation.Min(Amount(1)), true)
	requireServerOnly(t, validation.OneOf(json.Number("12")), true)
	requireServerOnly(t, validation.OneOf("YES"), false)
	requireServerOnly(t, validation.Min(uint64(9007199254740993)), false)
	requireServerOnly(t, validation.Same[string](), false)

	// Enum is different: its descriptor supplies validated wire values. That
	// lets membership describe "yes" while runtime validation checks "YES".
	descriptor := enum.Describe("app/models", "WireState", enum.Case[WireState]{Name: "Yes", Value: "YES"})
	requireServerOnly(t, validation.Enum(descriptor), false)
}

func TestCollectionMetadataDistinguishesArraysFromCustomAndByteEncoding(t *testing.T) {
	t.Parallel()
	requireServerOnly(t, validation.MinItems[[]string](1), false)
	requireServerOnly(t, validation.MaxItems[encodedItems](1), true)
	requireServerOnly(t, validation.MinItems[[]byte](1), true)
	requireServerOnly(t, validation.Distinct[[]byte](), true)
	requireServerOnly(t, validation.Each[[]byte](validation.Min(byte(1))), true)
	requireServerOnly(t, validation.Each[encodedItems](validation.NonBlank[string]()), true)
	requireServerOnly(t, validation.Distinct[[]WireState](), true)
	// A byte element encoder changes the standard byte-slice encoding into an
	// array. Its length remains meaningful even though its elements transform.
	requireServerOnly(t, validation.MinItems[[]encodedByte](1), false)
	requireServerOnly(t, validation.Distinct[[]encodedByte](), true)
}

func TestScalarComparisonsRejectUnboundedTextAndNonFiniteValues(t *testing.T) {
	t.Parallel()
	limits := validation.DefaultLimits()
	limits.ValueBytes = 3
	var bound *validation.LimitError
	err := validation.Same[string]().Check(t.Context(), validation.Pair[string]{Left: "long", Right: "long"}, limits)
	if !errors.As(err, &bound) {
		t.Fatal("comparison ignored text bound", err)
	}
	rejection(t, validation.Same[float64]().Check(t.Context(), validation.Pair[float64]{Left: math.Inf(1), Right: math.Inf(1)}, validation.DefaultLimits()))
	rejection(t, validation.Different[float64]().Check(t.Context(), validation.Pair[float64]{Left: math.NaN(), Right: 0}, validation.DefaultLimits()))
}
