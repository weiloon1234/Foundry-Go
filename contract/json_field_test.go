package contract

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type FieldLabels []string
type fieldModel struct{}

func (fieldModel) FoundryIdentity() (model.Identity, error) {
	panic("identity must not be called while describing a field")
}

func TestExplicitJSONFieldPreservesCollectionsAndNullability(t *testing.T) {
	schema := Schema{Root: "labels", Types: []Type{{ID: "labels", Kind: ArrayKind, Element: "text", Nullable: true}, {ID: "text", Kind: StringKind}}}
	descriptor := DefineJSONField[FieldLabels](schema)
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	schema.Types[0].Element = "changed"
	input, err := descriptor.Decode(t.Context(), []byte(`["one","two"]`), dtoLimits())
	if err != nil || !reflect.DeepEqual(input, FieldLabels{"one", "two"}) {
		t.Fatalf("named slice: %v %v", input, err)
	}
	data, err := descriptor.Encode(t.Context(), input, dtoLimits())
	if err != nil || string(data) != `["one","two"]` {
		t.Fatalf("encoding: %s %v", data, err)
	}
	rejected, err := descriptor.Decode(t.Context(), []byte(`["valid",123]`), dtoLimits())
	var invalid *DecodeError
	if !errors.As(err, &invalid) || rejected != nil || invalid.Issues()[0].Path != "/1" {
		t.Fatal("field contract lost all-or-zero indexed decoding", err)
	}
	nullable := DefineJSONField[value.Nullable[string]](Schema{Root: "nullable", Types: []Type{{ID: "nullable", Kind: AliasKind, Nullable: true, Element: "text"}, {ID: "text", Kind: StringKind}}})
	empty, err := nullable.Decode(t.Context(), []byte(`null`), dtoLimits())
	if err != nil || !empty.IsNull() {
		t.Fatal("explicit JSON null rejected", err)
	}
	present, err := nullable.Decode(t.Context(), []byte(`"name"`), dtoLimits())
	text, ok := present.Get()
	if err != nil || !ok || text != "name" {
		t.Fatal("nullable value changed", err)
	}
}

func TestExplicitJSONFieldKeepsExistingDTOAndModelBoundaries(t *testing.T) {
	schema := Schema{Root: "text", Types: []Type{{ID: "text", Kind: StringKind}}}
	if err := DefineJSONField[string](schema).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := DefineJSON[string](schema).Validate(); err == nil {
		t.Fatal("complete DTO constructor began accepting scalar declarations")
	}
	if err := DefineJSONValue[string](schema).Validate(); err == nil {
		t.Fatal("custom value constructor lost its native protocol requirement")
	}
	if err := DefineJSONField[fieldModel](schema).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("model field became a public JSON contract")
	}
	if err := DefineJSONField[**fieldModel](schema).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("model pointers bypassed the field boundary")
	}
	if err := DefineJSONField[string](Schema{}).Validate(); err == nil {
		t.Fatal("invalid graph accepted")
	}
}
