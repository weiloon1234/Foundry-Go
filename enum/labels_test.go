package enum_test

import (
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"testing"
)

func TestEnumLabelsReuseExactCaseMetadata(t *testing.T) {
	d := enum.Describe("example.test/domain", "Code", enum.Case[code]{Name: "Large", Value: 9007199254740993, LabelKey: "enum.code.large"})
	definitions, err := d.LabelDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	set, _ := i18n.NewLocaleSet("en", "en", "ms")
	c, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, definitions, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {"enum.code.large": {Text: "Besar"}}})
	if err != nil {
		t.Fatal(err)
	}
	label, err := d.Label(t.Context(), c, "ms", 9007199254740993)
	if err != nil || label != "Besar" {
		t.Fatal(label, err)
	}
	wire, err := d.Definition()
	if err != nil || wire.Cases[0].LabelKey != definitions[0].Key || string(wire.Cases[0].Value) != "9007199254740993" {
		t.Fatal(wire, err)
	}
	if _, err := d.LabelKey(0); err == nil {
		t.Fatal("unknown enum value got a label")
	}
	if enum.Describe("example.test/domain", "Code", enum.Case[code]{Name: "Bad", Value: 1, LabelKey: "Bad Key"}).Validate() == nil {
		t.Fatal("invalid label key")
	}
}

func TestEnumCasesCanShareOneCatalogLabel(t *testing.T) {
	d := enum.Describe("example.test/domain", "Code",
		enum.Case[code]{Name: "First", Value: 1, LabelKey: "enum.shared"},
		enum.Case[code]{Name: "Second", Value: 2, LabelKey: "enum.shared"},
	)
	definitions, err := d.LabelDefinitions()
	if err != nil || len(definitions) != 1 {
		t.Fatal(definitions, err)
	}
	locales, _ := i18n.NewLocaleSet("en", "en")
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, definitions, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"en": {"enum.shared": {Text: "Shared"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []code{1, 2} {
		if label, err := d.Label(t.Context(), catalog, "en", value); err != nil || label != "Shared" {
			t.Fatal(label, err)
		}
	}
	wire, err := d.Definition()
	if err != nil || len(wire.Cases) != 2 || wire.Cases[0].LabelKey != wire.Cases[1].LabelKey {
		t.Fatal(wire, err)
	}
}
