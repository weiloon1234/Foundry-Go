package validation_test

import (
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
	"testing"
)

func TestLocalizedLabelsPreserveWirePathsAndStaticFallback(t *testing.T) {
	set, _ := i18n.NewLocaleSet("en", "en", "ms")
	catalog, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, []i18n.MessageDefinition{{Key: "fields.name"}, {Key: "fields.confirm"}}, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {"fields.name": {Text: "Nama"}}})
	if err != nil {
		t.Fatal(err)
	}
	type input struct{ Name, Confirm string }
	name := validation.WithLabelKey(validation.WithLabel(validation.DefineField("name", func(v input) string { return v.Name }), "Name"), "fields.name")
	confirm := validation.WithLabelKey(validation.WithLabel(validation.DefineField("confirm", func(v input) string { return v.Confirm }), "Confirmation"), "fields.confirm")
	rule := validation.All(validation.Rules(name, validation.NonBlank[string]()), validation.Compare(confirm, name, validation.Same[string]()))
	issues := rejection(t, validation.Check(rule, t.Context(), input{Confirm: "private"}, validation.DefaultLimits()))
	localized, err := issues.LocalizeLabels(t.Context(), catalog, "ms")
	if err != nil {
		t.Fatal(err)
	}
	got := localized.Issues()
	if len(got) != 2 || got[0].Path != "/name" || got[0].Label != "Nama" || got[0].LabelKey != "fields.name" || got[1].Label != "Confirmation" || got[1].Path != "/confirm" {
		t.Fatal(got)
	}
	if issues.Issues()[0].Label != "Name" || got[0].Message != issues.Issues()[0].Message {
		t.Fatal("localization mutated original issue or approved message")
	}
	description, err := validation.DescribeRule(rule)
	if err != nil || description.Children[0].LabelKey != "fields.name" || description.Children[1].OtherLabelKey != "fields.name" {
		t.Fatal(description, err)
	}
	if validation.ValidateField(validation.WithLabelKey(name, "bad key")) == nil {
		t.Fatal("invalid label key")
	}
}
