package validation_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func messageCatalog(t testing.TB) *i18n.Catalog {
	t.Helper()
	locales, err := i18n.NewLocaleSet("ms", "ms", "ar")
	if err != nil {
		t.Fatal(err)
	}
	definitions := append(validation.MessageDefinitions(), i18n.MessageDefinition{Key: "fields.name"}, i18n.MessageDefinition{Key: "fields.confirmation"})
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{Fallback: "ar"}, definitions, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{
		"ms": {
			"fields.name": {Text: "Nama"}, "fields.confirmation": {Text: "Pengesahan"},
			"validation.min_length": {Forms: map[i18n.PluralForm]string{i18n.Other: "{{attribute}}: minimum {{min}} aksara."}},
			"validation.same":       {Text: "{{attribute}} mesti sama dengan {{other}}."},
		},
		"ar": {"fields.name": {Text: "الاسم"}, "validation.min_length": {Forms: map[i18n.PluralForm]string{i18n.One: "{{attribute}} واحد {{min}}", i18n.Two: "{{attribute}} اثنان {{min}}", i18n.Other: "{{attribute}} أحرف {{min}}"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
func TestLocalizedMessagesKeepNestedPathsLabelsOrderAndTruncation(t *testing.T) {
	t.Parallel()
	type input struct {
		Names              []string
		Name, Confirmation string
	}
	names := validation.DefineField("names/~", func(v input) []string { return v.Names }).WithLabel("Names").WithLabelKey("fields.name")
	name := validation.DefineField("name", func(v input) string { return v.Name }).WithLabel("Name").WithLabelKey("fields.name")
	confirmation := validation.DefineField("confirmation", func(v input) string { return v.Confirmation }).WithLabel("Confirmation").WithLabelKey("fields.confirmation")
	rule := validation.Parallel(names.Rules(validation.Each[[]string](validation.MinLength[string](2))), validation.Compare(confirmation, name, validation.Same[string]()), name.Rules(validation.NonBlank[string]()))
	limits := validation.DefaultLimits()
	limits.Issues = 2
	original := rejection(t, rule.Check(t.Context(), input{Names: []string{"x"}, Confirmation: "private-password"}, limits))
	if !original.Truncated() {
		t.Fatal("truncation lost")
	}
	catalog := messageCatalog(t)
	ms, err := original.Localize(t.Context(), catalog, "ms")
	if err != nil {
		t.Fatal(err)
	}
	issues := ms.Issues()
	if len(issues) != 2 || issues[0].Path != "/names~1~0/0" || issues[0].Message != "Nama: minimum 2 aksara." || issues[1].Message != "Pengesahan mesti sama dengan Nama." || !ms.Truncated() {
		t.Fatalf("localized issues: %+v", issues)
	}
	ar, err := ms.Localize(t.Context(), catalog, "ar")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Issues()[0].Message != "الاسم اثنان 2" || ar.Issues()[1].Label != "Confirmation" {
		t.Fatalf("repeat localization retained previous locale: %+v", ar.Issues())
	}
	if original.Issues()[0].Message != "Names must contain at least 2 characters." {
		t.Fatal("source was mutated")
	}
	encoded, _ := json.Marshal(issues)
	if strings.Contains(string(encoded), "private-password") {
		t.Fatal("input leaked")
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			locale := i18n.LocaleID("ms")
			if i%2 == 0 {
				locale = "ar"
			}
			got, err := original.Localize(t.Context(), catalog, locale)
			if err != nil || got.Issues()[0].Label == "Names" {
				t.Error("concurrent localization failed", err)
			}
		})
	}
	wg.Wait()
}
func TestMessageFallbackLiteralAndOwnedMetadata(t *testing.T) {
	t.Parallel()
	catalog := messageCatalog(t)
	rule := validation.MinLength[string](1)
	rejected := rejection(t, rule.Check(t.Context(), "", validation.DefaultLimits()))
	if rejected.Issues()[0].Message != "This field must contain at least 1 character." {
		t.Fatal(rejected.Issues())
	}
	fallback := rejection(t, validation.MaxLength[string](1).Check(t.Context(), "xx", validation.DefaultLimits()))
	localized, err := fallback.Localize(t.Context(), catalog, "ms")
	if err != nil || localized.Issues()[0].Message != "This field must contain at most 1 character." {
		t.Fatal(localized, err)
	}
	literal := rule.WithMessage("literal {{attribute}} {{unknown}}")
	result, err := rejection(t, literal.Check(t.Context(), "", validation.DefaultLimits())).Localize(t.Context(), catalog, "ms")
	if err != nil || result.Issues()[0].Message != "literal {{attribute}} {{unknown}}" {
		t.Fatal(result, err)
	}
	description, _ := rule.Description()
	description.Spec.Translation.Fallback[i18n.Other] = "changed"
	description.Spec.Translation.Arguments[0].Value = "changed"
	again, _ := rule.Description()
	if again.Spec.Translation.Fallback[i18n.Other] == "changed" || again.Spec.Translation.Arguments[0].Value == "changed" {
		t.Fatal("shared recipe")
	}
	definitions := validation.MessageDefinitions()
	definitions[0].Parameters[0].Name = "changed"
	if validation.MessageDefinitions()[0].Parameters[0].Name == "changed" {
		t.Fatal("shared definitions")
	}
}
func TestMessageCatalogRejectsConflictingSignatures(t *testing.T) {
	locales, _ := i18n.NewLocaleSet("en", "en")
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, []i18n.MessageDefinition{{Key: "validation.min_length"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	description, _ := validation.MinLength[string](2).Description()
	if validation.ValidateMessages(description, catalog) == nil {
		t.Fatal("mismatch accepted")
	}
	catalog, err = i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.ValidateMessages(description, catalog); err != nil {
		t.Fatal("optional key rejected", err)
	}
}
