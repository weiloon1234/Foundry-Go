package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
)

func TestPresentationNormalizesAliasesAndOwnsMetadata(t *testing.T) {
	schema := Schema{Root: "Input", Types: []Type{
		{ID: "Input", Kind: ObjectKind, Properties: []Property{{Name: "budget", Type: "optional", Presentation: Presentation{Kind: MoneyPresentation, LabelKey: "fields.budget", HelpKey: "help.budget"}}}},
		{ID: "optional", Kind: AliasKind, Element: "decimal", Nullable: true},
		{ID: "decimal", Kind: StringKind, Format: DecimalFormat},
	}}
	compiled, err := compileSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	schema.Types[0].Properties[0].Presentation.LabelKey = "changed"
	first := compiled.snapshot()
	first.Types[0].Properties[0].Presentation.Kind = FilePresentation
	if got := compiled.snapshot().Types[0].Properties[0].Presentation; got.Kind != MoneyPresentation || got.LabelKey != "fields.budget" {
		t.Fatal(got)
	}
	for name, hint := range map[string]Presentation{
		"unknown kind": {Kind: "script"}, "oversized": {LabelKey: i18n.MessageKey(strings.Repeat("a", MaxPresentationKeyBytes+1))},
		"control": {HelpKey: "bad\nkey"}, "incompatible text": {Kind: TextPresentation}, "file": {Kind: FilePresentation},
	} {
		t.Run(name, func(t *testing.T) {
			schema.Types[0].Properties[0].Presentation = hint
			if _, err := schema.Normalize(); err == nil {
				t.Fatal("invalid presentation accepted")
			}
		})
	}
}

func TestPresentationDoesNotOverrideEnumOrWireShape(t *testing.T) {
	for _, typ := range []Type{{Kind: IntegerKind, Bits: 64}, {Kind: StringKind, Format: UUIDFormat}, {Kind: StringKind, Cases: []json.RawMessage{json.RawMessage(`"secret"`)}}} {
		if (Presentation{Kind: PasswordPresentation}).ValidateType(typ) == nil {
			t.Fatal("password accepted incompatible codec")
		}
	}
	if (Presentation{Kind: PasswordPresentation}).ValidateType(Type{Kind: StringKind}) != nil {
		t.Fatal("legitimate credential input rejected")
	}
	if (Presentation{Kind: FilePresentation}).ValidateFile() != nil || (Presentation{Kind: MoneyPresentation}).ValidateFile() == nil {
		t.Fatal("file hint mismatch")
	}
}
