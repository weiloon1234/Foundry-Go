package contract

import (
	"encoding/json"
	"slices"
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

func TestPresentationKeyBoundAndKindSetHaveOneOwner(t *testing.T) {
	exact := i18n.MessageKey(strings.Repeat("a", MaxPresentationKeyBytes))
	if (Presentation{LabelKey: exact}).Validate() != nil {
		t.Fatal("a key at the published bound was rejected")
	}
	if (Presentation{HelpKey: exact + "a"}).Validate() == nil {
		t.Fatal("a key above the published bound was accepted")
	}
	kinds := PresentationKinds()
	for _, kind := range kinds {
		if (Presentation{Kind: kind}).Validate() != nil {
			t.Fatal("listed kind rejected", kind)
		}
	}
	kinds[0] = "script"
	if slices.Contains(PresentationKinds(), "script") || (Presentation{Kind: "script"}).Validate() == nil {
		t.Fatal("caller mutation changed the closed kind set")
	}
}

func TestPasswordTypesFollowEveryOutputPath(t *testing.T) {
	password := Presentation{Kind: PasswordPresentation}
	types := []Type{
		{ID: "secret", Kind: ObjectKind, Properties: []Property{{Name: "password", Type: "string", Presentation: password}}},
		{ID: "list", Kind: ArrayKind, Element: "secret"},
		{ID: "alias", Kind: AliasKind, Element: "list"},
		{ID: "choice", Kind: UnionKind, Discriminator: "kind", Variants: []Variant{{Tag: "secret", Type: "secret"}}},
		{ID: "tree", Kind: ObjectKind, Properties: []Property{{Name: "self", Type: "tree"}, {Name: "choice", Type: "choice"}}},
		{ID: "plain", Kind: ObjectKind, Properties: []Property{{Name: "name", Type: "string", Presentation: Presentation{Kind: TextPresentation}}}},
		{ID: "string", Kind: StringKind},
	}
	sensitive := PasswordTypes(types)
	for _, id := range []TypeID{"secret", "list", "alias", "choice", "tree"} {
		if !sensitive[id] {
			t.Fatal("password graph not reached through", id)
		}
	}
	if sensitive["plain"] || sensitive["string"] {
		t.Fatal("unrelated types were marked sensitive")
	}
}

func TestPresentationRegistrationNamesTheContradictingField(t *testing.T) {
	schema := Schema{Root: "Input", Types: []Type{
		{ID: "Input", Kind: ObjectKind, Properties: []Property{{Name: "count", Type: "integer", Presentation: Presentation{Kind: EmailPresentation}}}},
		{ID: "integer", Kind: IntegerKind, Bits: 64},
	}}
	if _, err := schema.Normalize(); err == nil || !strings.Contains(err.Error(), "Input.count") {
		t.Fatal("contradiction did not name its field", err)
	}
}
