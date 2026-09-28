package validation_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestSerializedValidationPreservesCompositionAndOwnsParameters(t *testing.T) {
	rule := validation.When(validation.Min(1), validation.Bail(validation.Max(9), validation.OneOf(2, 4, 6)))
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var restored validation.Description
	if err := json.Unmarshal(wire, &restored); err != nil {
		t.Fatal(err)
	}
	normalized, err := restored.Normalize()
	if err != nil || !reflect.DeepEqual(normalized, info) {
		t.Fatal("description changed", err)
	}
	normalized.Children[0].Spec.Parameters[0].Value[0] = '8'
	if string(restored.Children[0].Spec.Parameters[0].Value) != "1" {
		t.Fatal("metadata aliases input")
	}
	for _, invalid := range []validation.Description{{}, {Kind: validation.LeafKind}, {Kind: validation.AllKind}, {Kind: validation.WhenKind, Children: []validation.Description{info}}, {Kind: validation.FieldKind, Children: []validation.Description{info}}} {
		if _, err := invalid.Normalize(); err == nil {
			t.Fatal("malformed rule metadata accepted")
		}
	}
}
