package codec_test

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func TestSensitiveMetadataSurvivesAdaptersWithoutChangingBinding(t *testing.T) {
	ordinary := codec.String[string]()
	sensitive := ordinary.WithSensitiveValues()
	validated := sensitive.Validated(func(string) error { return nil }).WithParameterType(codec.TypeText)
	nullable := codec.Nullable(validated).Validated(func(value.Nullable[string]) error { return nil })
	if ordinary.SensitiveValues() || !sensitive.SensitiveValues() || !validated.SensitiveValues() || !nullable.SensitiveValues() {
		t.Fatal("metadata aliased or lost")
	}
	if bound, err := nullable.Bind(value.Of("private-value")); err != nil || bound != "private-value" {
		t.Fatal("sensitivity changed persistence", err)
	}
}
