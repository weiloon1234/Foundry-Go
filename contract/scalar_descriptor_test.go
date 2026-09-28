package contract

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
)

type ScalarState string

func TestScalarDescriptorNormalizesAndOwnsEnumMetadata(t *testing.T) {
	descriptor := enum.Describe("foundry.test/contracts", "ScalarState",
		enum.Case[ScalarState]{Name: "Draft", Value: "draft"},
		enum.Case[ScalarState]{Name: "Ready", Value: "ready"})
	scalar := DefineScalar[ScalarState](EnumType("foundry.test/contracts.ScalarState", descriptor))
	first, err := scalar.Description()
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != StringKind || len(first.Cases) != 2 || string(first.Cases[0]) != "\"draft\"" {
		t.Fatalf("enum scalar metadata: %+v", first)
	}
	first.Cases[0][1] = 'x'
	first.ID = "changed"
	second, err := scalar.Description()
	if err != nil || second.ID != "foundry.test/contracts.ScalarState" || string(second.Cases[0]) != "\"draft\"" {
		t.Fatal("description exposed owned enum metadata", err)
	}
	input := Type{ID: "rank", Kind: IntegerKind, Signed: true, Cases: []json.RawMessage{[]byte("2"), []byte("1")}}
	rank := DefineScalar[int](input)
	input.Cases[0][0] = '9'
	description, err := rank.Description()
	if err != nil || description.Bits != uint8(strconv.IntSize) || !reflect.DeepEqual(description.Cases, []json.RawMessage{[]byte("1"), []byte("2")}) {
		t.Fatalf("integer normalization/ownership: %+v %v", description, err)
	}
}

func TestScalarDescriptorRejectsNonScalarAndInvalidMetadata(t *testing.T) {
	for name, description := range map[string]Type{
		"unset":          {},
		"nullable":       {ID: "v", Kind: StringKind, Nullable: true},
		"object":         {ID: "v", Kind: ObjectKind},
		"array":          {ID: "v", Kind: ArrayKind, Element: "v"},
		"alias":          {ID: "v", Kind: AliasKind, Element: "v"},
		"dynamic":        {ID: "v", Kind: DynamicKind},
		"integer_width":  {ID: "v", Kind: IntegerKind, Bits: 7},
		"unknown_format": {ID: "v", Kind: StringKind, Format: "invented"},
		"wrong_case":     {ID: "v", Kind: IntegerKind, Cases: []json.RawMessage{[]byte("\"string\"")}},
		"duplicate_case": {ID: "v", Kind: StringKind, Cases: []json.RawMessage{[]byte("\"a\""), []byte("\"a\"")}},
	} {
		t.Run(name, func(t *testing.T) {
			scalar := DefineScalar[string](description)
			if scalar.Validate() == nil {
				t.Fatal("invalid scalar declaration accepted")
			}
			if value, err := scalar.Description(); err == nil || value.ID != "" {
				t.Fatal("invalid declaration exposed metadata")
			}
		})
	}
	var zero Scalar[int]
	if zero.Validate() == nil {
		t.Fatal("zero scalar descriptor accepted")
	}
}
