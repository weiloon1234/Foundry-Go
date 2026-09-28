package validation_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestFieldSelectionPreservesDeclaredNameTypeAndNull(t *testing.T) {
	type row struct{ Label value.Nullable[string] }
	calls := 0
	field := validation.DefineField("display_label", func(input row) value.Nullable[string] { calls++; return input.Label })
	if field.Name() != "display_label" {
		t.Fatal("wire name was rediscovered")
	}
	for _, input := range []value.Nullable[string]{value.Null[string](), value.Of(""), value.Of("label")} {
		selected, err := field.Select(row{Label: input})
		if err != nil || selected != input {
			t.Fatal("selector changed null or concrete value", err)
		}
	}
	if calls != 3 {
		t.Fatal("selector did not execute exactly once per call")
	}
	for _, bad := range []validation.Field[row, string]{{}, validation.DefineField[row, string]("label", nil), validation.DefineField("", func(row) string { t.Fatal("invalid selector executed"); return "" })} {
		if selected, err := bad.Select(row{}); selected != "" || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid field exposed a value", err)
		}
	}
}
